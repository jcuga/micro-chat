package main

import (
	"flag"
	"github.com/jcuga/golongpoll"
	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday"
	"html/template"
	"log"
	"net/http"
	"regexp"
	"strings"
)

const (
	ALL_CHATS = "all_chats"
)

func main() {
	listenAddress := flag.String("addr", ":8080", "address:port to serve.")
	maxChatLifeHours := flag.Uint("maxChatHrs", 24, "how long chats are stored (hours)")
	topicRefreshSeconds := flag.Uint("topicRefreshSec", 30, "how often the popular/recent topic boards are refreshed in browser (seconds)")
	maxTopicListNum := flag.Uint("maxTopicLists", 10, "how many topics listed in top popular/recent topics")
	numChatsOnScreen := flag.Uint("chatsOnScreen", 50, "How many chats to display on a screen.")
	if *maxChatLifeHours < 1 {
		log.Fatalf("maxChatHrs cmdline arg must be >= 1\n")
	}
	if *topicRefreshSeconds < 1 {
		log.Fatalf("topicRefreshSec cmdline arg must be >= 1\n")
	}
	if *maxTopicListNum < 1 {
		log.Fatalf("maxTopicLists cmdline arg must be >= 1\n")
	}
	if *numChatsOnScreen < 1 {
		log.Fatalf("chatsOnScreen cmdline arg must be >= 1\n")
	}
	flag.Parse()

	// Our chat server is just a longpoll/pub-sub server.
	manager, err := golongpoll.StartLongpoll(golongpoll.Options{
		// make more than we show so we can collect stats by topic further back
		MaxEventBufferSize:     int(*numChatsOnScreen) * 10,
		EventTimeToLiveSeconds: int(*maxChatLifeHours) * 60 * 60,
	})
	if err != nil {
		log.Fatalf("Failed to create chat longpoll manager: %q\n", err)
	}

	http.HandleFunc("/", getIndexClosure(*maxChatLifeHours,
		*topicRefreshSeconds, *maxTopicListNum, *numChatsOnScreen))
	http.HandleFunc("/post", getChatPostClosure(manager))
	http.HandleFunc("/subscribe", manager.SubscriptionHandler)

	log.Printf("addr:%v, maxChatHrs:%v, topicRefreshSec:%v, maxTopicLists:%v chatsOnScreen:%v\n",
		*listenAddress, *maxChatLifeHours, *topicRefreshSeconds, *maxTopicListNum, *numChatsOnScreen)
	log.Printf("Launching chat server on %s\n", *listenAddress)
	http.ListenAndServe(*listenAddress, nil)
}

type ChatPost struct {
	DisplayName string `json:"display_name"`
	Message     string `json:"message"`
	Topic       string `json:"topic"`
}

func truncateInput(input string, maxlen int) string {
	output := []rune(input)
	if len(output) > maxlen {
		return string(output[:maxlen])
	}
	return string(output)
}

func sanitizeInput(input string) string {
	return bluemonday.UGCPolicy().Sanitize(input)
}

func toMarkdown(input string) string {
	html := blackfriday.MarkdownBasic([]byte(input))
	return string(html[:])
}

// Create a closure that contains a ref to our longpoll manager so we can
// call Publish() from within web handler
// NOTE: the manager is safe to call this way because it relies on channels
func getChatPostClosure(manager *golongpoll.LongpollManager) func(w http.ResponseWriter, r *http.Request) {
	reg, err := regexp.Compile("[^A-Za-z0-9]+")
	if err != nil {
		log.Fatal("Error compiling regexp: ", err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		if r.Method != "POST" {
			http.Error(w, "Invalid request method.", 405)
			return
		}
		err := r.ParseForm()
		if err != nil {
			http.Error(w, "Invalid form data.", 405)
			return
		}
		topic := r.PostFormValue("topic")
		topic = normalizeTopic(topic, reg)
		display_name := r.PostFormValue("display_name")
		message := r.PostFormValue("message")
		if len(strings.TrimSpace(topic)) == 0 || len(strings.TrimSpace(display_name)) == 0 ||
			len(strings.TrimSpace(message)) == 0 {
			http.Error(w, "Invalid request.  Blank/Invalid topic (must be A-Za-z0-9), display_name, or message.", 400)
			return
		}
		// enforce max lengths--note strings could be non-ascii so treat as runes
		topic = truncateInput(topic, 48) // topic sanitized by normalization func that only allows A-Za-z0-9space
		display_name = sanitizeInput(truncateInput(display_name, 28))
		message = sanitizeInput(toMarkdown(truncateInput(message, 512)))
		chat := ChatPost{DisplayName: display_name, Message: message, Topic: topic}
		manager.Publish(topic, chat)
		// show on the all-chats channel as well that shows on the homepage when you
		// haven't filtered to a specific topic.
		manager.Publish(ALL_CHATS, chat)
		// redirect to the chat page for the given topic
		if r.PostFormValue("doAjax") == "yes" {
			// ajax post, return ok
			w.Write([]byte("ok"))
			return
		} else {
			// form post, do Redirect
			http.Redirect(w, r, "/?topic="+topic+"&display_name="+display_name, http.StatusSeeOther)
		}
	}
}

func getIndexClosure(maxChatLifeHours, topicRefreshSeconds, maxTopicListNum, numChatsOnScreen uint) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		if r.Method != "GET" {
			http.Error(w, "Invalid request method.", 405)
			return
		}
		topic := r.URL.Query().Get("topic")
		displayName := r.URL.Query().Get("display_name")
		t := template.New("chat_homepage")
		t, _ = t.Parse(getIndexTemplateString())
		templateData := struct {
			Topic               string
			DisplayName         string
			AllChats            string
			MaxChatLifeHours    uint
			TopicRefreshSeconds uint
			MaxTopicListNum     uint
			NumChatsOnScreen    uint
		}{topic, displayName, ALL_CHATS, maxChatLifeHours, topicRefreshSeconds,
			maxTopicListNum, numChatsOnScreen}
		t.Execute(w, templateData)
	}
}

func normalizeTopic(topic string, reg *regexp.Regexp) string {
	norm := reg.ReplaceAllString(topic, "-")
	norm = strings.Trim(norm, "-")
	return norm
}

func logRequest(r *http.Request) {
	topic := ""
	displayName := ""
	if r.Method == "GET" {
		topic = r.URL.Query().Get("topic")
		displayName = r.URL.Query().Get("display_name")
	} else if r.Method == "POST" {
		topic = r.PostFormValue("topic")
		displayName = r.PostFormValue("display_name")
	}
	log.Printf("HTTP %s %s  topic: %s, display_name: %s src_ip: %s x_forwarded_for: %s\n",
		r.Method, r.URL.Path, topic, displayName, r.RemoteAddr, r.Header.Get("X-FORWARDED-FOR"))
}

func getIndexTemplateString() string {
	return `<html>
    <head>
      <title>micro-chat</title>
			<meta name="viewport" content="width=device-width, initial-scale=1.0">
			<link rel="stylesheet" type="text/css" href="https://cdnjs.cloudflare.com/ajax/libs/skeleton/2.0.4/skeleton.min.css">
			<style>
				body {
					font-size: 1.7rem;
					line-height: 1.4;
			  }
				a.other-topic, a.topic {
					font-size: 1.8rem;
					color: #00AA00;
				}

				div#feedback {
				  color: red;
					font-style: italic;
					margin: 0;
					padding: 0;
					font-size: 1.3rem;
			  }
				time.timeago {
						font-size: 1.4rem;
						color: #999999
  			}
				div.displayName {
					font-size: 1.5rem;
					color: #FF8888;
					font-weight: bold;
					font-style: italic;
			  }
				div.chat {
					margin: 0 0 0.5rem 0;
					padding: 0.6rem;
					border-style: solid;
			    border-width: 1px;
					border-color: #AAAAAA;
					border-radius: 1.0rem;
					box-shadow: 0 0.2rem 0.4rem 0 rgba(0, 0, 0, 0.2), 0 0.2rem 0.8rem 0 rgba(0, 0, 0, 0.19);
  			}
				div.msg p {
					margin: 0 0 0.5rem 0;
					padding: 0;
				}

				div.chat img {
		  		width: 100%;
    	    height: auto;
  			}
				h1 {
				   font-size: 3.0rem;
			  }
				h2 {
				   font-size: 2.4rem;
			  }
				h3 {
				   font-size: 2.0rem;
			  }
				h4, h5, h6 {
				   font-size: 1.7rem;
			  }
				h1, h2, h3, h4, h5, h6 {
			  		margin-bottom: 0.4rem;
			  }
				div.chat h1, div.chat h2, div.chat h3, div.chat h4, div.chat h5, div.chat h6 {
					font-weight: bold;
				}
				li {
				  margin-bottom: 0rem;
				}
				div.msg a {
				  font-style: italic;
					font-weight: bold;
			  }
				#content-container {
					width: 100%;
				}
				#content-container .chat-stream {
					min-width: 280px;
				}
				body {
				  margin: 0.8rem 0 0.8rem 1.0rem;
				  padding: 0;
 			  }
				@media (max-width: 700px) {
					#content-container .column, #content-container .columns {
							margin-left: 0;
					}
				}
				@media (max-width: 600px) {
					body {
						margin-left: 0.2rem;
					}
				}
				#popular_topics_list div.msg, #recent_topics_list div.msg {
					text-overflow: ellipsis;
					overflow: hidden;
					max-height: 40%;
			  }
				textarea {
					display: block;
					width: 100%;
					resize: vertical;
				}
				input[type='text'],
				textarea {
				  font-size: 1.7rem;
					margin-bottom: 1.0rem;
				}
				form {
				   margin-bottom: 1.0rem;
			  }
				hr {
					margin-top: 0.5rem;
					margin-bottom: 1.5rem;
				}
				div.msg {
					overflow-y: hidden;
				}
				#displayNameAlready {
					display: inline-block;
					color: #FF8888;
					margin: 0.5rem;
					padding: 0.5rem;
				  font-size: 1.7rem;
					font-weight: bold;
					font-style: italic;
			  }
				#changeDisplayName {
					color: #00AA00;
			  }
				#changeDisplayName:hover {
					text-decoration: underline;
			  }
				#footer {
					font-size: 1.4rem;
					color: #AAAAAA;
					padding: 1rem;
					margin: auto;
					display: block;
					text-align: center;
  			}
				@media only screen and (max-width: 760px) {
				  #mobileCanary { display: none; }
				}
				#recent_topics, #popular_topics, #chats_list {
					margin-bottom: 3.0rem;
				}

				span.txtMarkup {
					margin-left: 0.1rem;
					padding: 0.6rem;
				}
				span.txtMarkup:hover {
					color: #FF0000;
					cursor: pointer;
				}

			</style>
			<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/4.6.3/css/font-awesome.css">
    	<script src="http://code.jquery.com/jquery-1.11.3.min.js"></script>
			<script src="https://cdnjs.cloudflare.com/ajax/libs/jquery-timeago/1.5.3/jquery.timeago.min.js"></script>

    </head>
    <body>

			<div id="content-container" class="container">
			<!-- just use a number and class 'column' or 'columns' -->

			<div class="row">

		    <div class="six columns chat-stream">
					{{ if .Topic }}
		        <h2 id="chat-topic-hdr"><i class="fa fa-comments"></i> {{ .Topic}}</h2>
						<a class="other-topic" href="/">Select other topic.</a>
		      {{ else }}
		        <h2 id="chat-topic-hdr"><i class="fa fa-comments"></i> Latest chats</h2>
		      {{ end }}
					<hr />
					<form id="chatForm" method="POST" action="/post">
						{{ if .Topic }}
						  <input type="hidden" id="topic" name="topic" value="{{ .Topic }}">
						{{ else }}
						  <label for="topic">Topic:</label><input type="text" maxlength="48" id="topic" name="topic">
						{{ end }}
						<label id="nameLbl" for="display_name">Post as</label>
						{{ if .DisplayName }}
						<span id="displayNameAlready"><i class="fa fa-user"></i> {{.DisplayName}}</span><span id="changeDisplayName">[Change]</span>
						<input id="displayName" type="hidden" name="display_name" value="{{.DisplayName}}">
						{{ else }}
						<input id="displayName" type="text" maxlength="28" name="display_name" value="">
						<label id="lblForMsg" for="message">Message</label>
						{{ end }}
						<textarea id="msgArea" name="message" maxlength="512"></textarea>
						{{ if .Topic }}
						  <!-- dynamic page instead of form post/redirect -->
							<button id="chat-btn" type="button">Post</button>
						{{ else }}
							<input id="chat-submit" type="submit" value="post">
						{{ end }}
						<span id="addPicture" title="Add Picture" class="txtMarkup"><i class="fa fa-photo"></i></span>
						<span id="addLink" title="Add Link" class="txtMarkup"><i class="fa fa-link"></i></span>
						<span id="addHeader" title="Add Header" class="txtMarkup"><i class="fa fa-header"></i></span>
						<span id="addList" title="Add List" class="txtMarkup"><i class="fa fa-list-ul"></i></span>
						<span id="markdownHelp" title="How to use Markdown" class="txtMarkup"><i class="fa fa-question"></i></span>

						<div id="feedback"></div>
					</form>

		      <div id="chats_list">
						<div id="noChatsYet"><i class="fa fa-refresh fa-spin" aria-hidden="true"></i> Waiting for first chat.</div>
		      </div>
				</div>

				<div class="three columns">
					<div id="recent_topics">
						<h2 id="recent-topic-hdr"><i class="fa fa-comments"></i> Recent</h2>
					<hr />
						<div id="recent_topics_list">
							<span class="nothing-yet"><i class="fa fa-refresh fa-spin" aria-hidden="true"></i></span>
						</div>
		      </div>
				</div>

				<div class="three columns">
					<div id="popular_topics">
						<h2 id="popular-topic-hdr"><i class="fa fa-comments"></i> Popular</h2>
					<hr />
						<div id="popular_topics_list">
							<span class="nothing-yet"><i class="fa fa-refresh fa-spin" aria-hidden="true"></i></span>
						</div>
		      </div>
				</div>

		  </div>

			</div>
			<div id="footer">&copy; Urmom Lol 2016</div>
			<div id="mobileCanary"></div>

      <script>
          // for browsers that don't have console
          if(typeof window.console == 'undefined') { window.console = {log: function (msg) {} }; }

          // Start checking for any events that occurred within 24 hours minutes prior to page load
          // so we display recent chats:
          var sinceTime = (new Date(Date.now() - ({{.MaxChatLifeHours}} * 60 * 60 * 1000))).getTime();
          // subscribe to a specific topic or all chats
					var category = "{{ if .Topic }}{{ .Topic }}{{ else }}{{ .AllChats }}{{ end }}";

					// for current page of chats--could be either specific category or all
					// chats
          (function poll() {
              var timeout = 50;  // in seconds
              var optionalSince = "";
              if (sinceTime) {
                  optionalSince = "&since_time=" + sinceTime;
              }
              var pollUrl = "/subscribe?timeout=" + timeout + "&category=" + category + optionalSince;
              // how long to wait before starting next longpoll request in each case:
              var successDelay = 10;  // 10 ms
              var errorDelay = 3000;  // 3 sec
							var maxChats = {{.NumChatsOnScreen}};
              $.ajax({ url: pollUrl,
                  success: function(data) {
											$("#noChatsYet").remove();
											if (data && data.events && data.events.length > 0) {
                          // got events, process them
                          // NOTE: these events are in chronological order (oldest first)
													var startIndex = 0;
													// don't load more than max number of chats per screen:
													if (data.events.length > maxChats) {
														startIndex = data.events.length - maxChats;
													}
                          for (var i = startIndex; i < data.events.length; i++) {
                              // Display event
                              var event = data.events[i];
															var msgDate = new Date(event.timestamp);
															var timestamp = "<time class=\"timeago\" datetime=\"" + msgDate.toISOString() + "\">"+msgDate.toLocaleTimeString()+"</time>";
															var topicPart = ""
															// only show topic link if its not our current topic
															if (event.data.topic !== "{{.Topic}}") {
																topicPart = "<div class=\"topic\"><a class=\"topic\" href='/?topic=" + event.data.topic + "'><i class=\"fa fa-comments\"></i> " + event.data.topic + "</a></div>"
															}
															$("#chats_list").prepend(
																	"<div class=\"chat\">" + topicPart + "<div class=\"msg\">" + event.data.message + "</div><div class=\"displayName\"><i class=\"fa fa-user\"></i> " + event.data.display_name + "</div><div class=\"postTime\">"  + timestamp +  "</div></div>"
															)
															jQuery("time.timeago").timeago();
                              // Update sinceTime to only request events that occurred after this one.
                              sinceTime = event.timestamp;
                          }
													// make sure our displayed chats doesn't exceed our
													// max on screen
													var excessChats = $("#chats_list > div").length - maxChats;
													if (excessChats > 0) {
														// remove excess
														$('#chats_list > div').slice(-1 * excessChats).remove();
													}
													// success!  start next longpoll
                          setTimeout(poll, successDelay);
                          return;
                      }
                      if (data && data.timeout) {
                          console.log("No events, checking again.");
                          // no events within timeout window, start another longpoll:
                          setTimeout(poll, successDelay);
                          return;
                      }
                      if (data && data.error) {
                          console.log("Error response: " + data.error);
                          console.log("Trying again shortly...")
                          setTimeout(poll, errorDelay);
                          return;
                      }
                      // We should have gotten one of the above 3 cases:
                      // either nonempty event data, a timeout, or an error.
                      console.log("Didn't get expected event data, try again shortly...");
                      setTimeout(poll, errorDelay);
                  }, dataType: "json",
              error: function (data) {
                  console.log("Error in ajax request--trying again shortly...");
                  setTimeout(poll, errorDelay);  // 3s
              }
              });
          })();

					// less frequent longpoll for all chats so we can populate the widgets
					// showing recent topics and most popular topics
					(function checkTopics() {
              var timeout = 50;  // in seconds
							// always fetch all chats during last N seconds
							// we don't update subsequent calls to timestamp of most
							// recent event because we're always fetching list of
							// recent, and not only ones since last call...
							var topicSinceTime = (new Date(Date.now() - ({{.MaxChatLifeHours}} * 60 * 60 * 1000))).getTime();
              var topicsSince = "&since_time=" + topicSinceTime;
              var pollUrl = "/subscribe?timeout=" + timeout + "&category=" + {{ .AllChats }} + topicsSince;
              // how long to wait before starting next longpoll request in each case:
							// these are spread out more than regular chat poll since this is
							// just show show pretty features like recent topics/popular topics
            	var successDelay = ({{.TopicRefreshSeconds}} * 1000);
              var errorDelay = 60000;  // 30 sec
							// number of topics in our Top Recent/Top Active iists
							var maxNumTopics = {{.MaxTopicListNum}};
              $.ajax({ url: pollUrl,
                  success: function(data) {
                      if (data && data.events && data.events.length > 0) {
                          // got events, process them
                          // NOTE: these events are in chronological order (oldest first)
													// let's inspect recent chats to determine popular
													// and recent topics
													var numChatsPerTopic = { };
													var lastTimestampPerTopic = { };
	                        for (var i = 0; i < data.events.length; i++) {
                              var event = data.events[i];
															if (numChatsPerTopic[event.data.topic]) {
 													      numChatsPerTopic[event.data.topic][0]++;
 													      numChatsPerTopic[event.data.topic][1] = event;
	 												    }
	 													  else {
	 													    numChatsPerTopic[event.data.topic] = [1, event];
	 													  }
															// since chats are oldest first, just keep track of last seen timestamp
															// and when we get to the end we'll have most recent timestamp for each topic
	 													  lastTimestampPerTopic[event.data.topic] = [event.timestamp, event];
															// NOTE: we don't update since time here based on
															// event time stamps. we always fetch all chats within last N seconds
                          }
													// Populate our panels showing recent/popular topics
													var sortableTopicCounts = [];
													var sortableTopicTimes = [];
													for (var topic in numChatsPerTopic) {
												      sortableTopicCounts.push([topic, numChatsPerTopic[topic]])
													}
													for (var topic in lastTimestampPerTopic) {
												      sortableTopicTimes.push([topic, lastTimestampPerTopic[topic]])
													}
													sortableTopicTimes.sort(
													    function(a, b) {
																return b[1][0] - a[1][0];
													    }
													)
													sortableTopicCounts.sort(
													    function(a, b) {
													        return b[1][0] - a[1][0];
													    }
													)
													// update topic letterboards
													if (sortableTopicTimes.length > 0) {
														$("#recent_topics_list").empty();
														for (var i = 0; i < sortableTopicTimes.length && i < maxNumTopics; i++) {
															var event = sortableTopicTimes[i][1][1];
															var msgDate = new Date(event.timestamp);
															var timestamp = "<time class=\"timeago\" datetime=\"" + msgDate.toISOString() + "\">"+msgDate.toLocaleTimeString()+"</time>";
															var chatHtml = "<div class=\"chat\"><div class=\"topic\"><a class=\"topic\" href=\"/?topic=" + sortableTopicTimes[i][0] + "\"><i class=\"fa fa-comments\"></i> " + sortableTopicTimes[i][0]  + "</a></div><div class=\"msg\">" + event.data.message + "</div><div class=\"displayName\"><i class=\"fa fa-user\"></i> " + event.data.display_name + "</div><div class=\"postTime\">"  + timestamp +  "</div></div>"
															$("#recent_topics_list").append("<div class=\"topic-item\">" + chatHtml + "</div>");
														}
													}
													if (sortableTopicCounts.length > 0) {
														$("#popular_topics_list").empty();
														for (var i = 0; i < sortableTopicCounts.length && i < maxNumTopics; i++) {
															var event = sortableTopicCounts[i][1][1];
															var msgDate = new Date(event.timestamp);
															var timestamp = "<time class=\"timeago\" datetime=\"" + msgDate.toISOString() + "\">"+msgDate.toLocaleTimeString()+"</time>";
															var chatHtml = "<div class=\"chat\"><div class=\"topic\">(" + sortableTopicCounts[i][1][0] + ") <a class=\"topic\" href=\"/?topic=" + sortableTopicCounts[i][0]  + "\"><i class=\"fa fa-comments\"></i> " + sortableTopicCounts[i][0]  + "</a></div><div class=\"msg\">" + event.data.message + "</div><div class=\"displayName\"><i class=\"fa fa-user\"></i> " + event.data.display_name + "</div><div class=\"postTime\">"  + timestamp +  "</div></div>"
															$("#popular_topics_list").append("<div class=\"topic-item\">" + chatHtml + "</div>");
														}
													}
													// update timestamps:
													jQuery("time.timeago").timeago();

													// success!  start next longpoll
                          setTimeout(checkTopics, successDelay);
                          return;
                      }
                      if (data && data.timeout) {
                          console.log("No events, checking again.");
                          // no events within timeout window, start another longpoll:
                          setTimeout(checkTopics, successDelay);
                          return;
                      }
                      if (data && data.error) {
                          console.log("Error response: " + data.error);
                          console.log("Trying again shortly...")
                          setTimeout(checkTopics, errorDelay);
                          return;
                      }
                      // We should have gotten one of the above 3 cases:
                      // either nonempty event data, a timeout, or an error.
                      console.log("Didn't get expected event data, try again shortly...");
                      setTimeout(checkTopics, errorDelay);
                  }, dataType: "json",
              error: function (data) {
                  console.log("Error in ajax request--trying again shortly...");
                  setTimeout(checkTopics, errorDelay);  // 3s
              }
              });
          })();

					$("#chat-btn").click(function() {
						$("#chat-btn").attr("disabled", "disabled");
						$("#displayName").attr("disabled", "disabled");
						$("#msgArea").attr("disabled", "disabled");
						$("#chatForm").addClass("sending");
						$("#feedback").empty();
						var dname = $("#displayName").val();
						var msg = $("#msgArea").val();
						var t = $("#topic").val();
						$.ajax({
						  type: 'POST',
						  url: "/post",
						  data: {
 								doAjax: "yes", topic: t, display_name: dname, message: msg
						  },
						  success: function(data){
								$("#chatForm").removeClass("sending");
								$("#displayName").removeAttr('disabled');
								$("#msgArea").removeAttr('disabled');
								$("#msgArea").val('');
								$("#msgArea").focus();
								$("#chat-btn").removeAttr('disabled');
								$("#lblForMsg").hide();
								if ($("#displayName").is(':visible')) {
									$("#displayName").hide();
									$("#displayName").before("<span id=\"displayNameAlready\"><i class=\"fa fa-user\"></i> " + dname + "</span><span id=\"changeDisplayName\">[Change]</span>");
									// re-bind click handler to new reset name button
									$("#changeDisplayName").click(clickToChangeNameFunc)
								}
						  },
						  error: function(xhr, textStatus, error){
								$("#chatForm").removeClass("sending");
								$("#displayName").removeAttr('disabled');
								$("#msgArea").removeAttr('disabled');
								$("#msgArea").focus();
								$("#chat-btn").removeAttr('disabled');
								$("#feedback").html("<span>" + xhr.responseText + "</span>");
						  }
						});
					});

					$("#msgArea").keypress(function(event) {
					    if (event.which == 13 && !event.shiftKey) {
								if ($("#mobileCanary").css('display')=='none') {
										// don't submit, this is likely mobile device and you can't use
										// the shift key
								} else {
									event.preventDefault();
					        $("#chat-submit").click();
									$("#chat-btn").click();
								}
					    }
					});

					jQuery(document).ready(function() {
					  jQuery("time.timeago").timeago();
						// focus on most pertinent input element
						if ($("#topic").is(':visible')) {
							$("#topic").focus();
						} else if ($("#displayName").is(':visible'))  {
							$("#displayName").focus();
						} else {
								$("#msgArea").focus();
						}
					});

					var clickToChangeNameFunc = function(){
						$("#displayNameAlready").remove();
						$("#changeDisplayName").remove();
						// normally you cant change the input type on the fly, but see:
						// http://stackoverflow.com/questions/3541514/jquery-change-input-type
						// for why this works
						$('#displayName').clone().attr('type','text').insertAfter('#displayName').prev().remove();
						$('#displayName').show();
						$('#displayName').focus();
			  	};
					$("#changeDisplayName").click(clickToChangeNameFunc)

					$("#addPicture").click(function() {
						var picUrl = prompt("Enter picture's URL", "");
						if (picUrl != null && picUrl.length > 0) {
   							$('#msgArea').val( $('#msgArea').val() + '\n![](' + picUrl + ')\n' );
                setTimeout(function() {
									// put focus at end of textarea
									var text = $("#msgArea").val();
									$("#msgArea").focus().val("").val(text);
								}, 100);
						}
					});
					$("#addLink").click(function() {
						var linkUrl = prompt("Enter Link's URL", "");
						if (linkUrl != null && linkUrl.length > 0) {
							var linkText = prompt("Enter Link's Text (optional)", "");
							if(linkText == null || linkText.length == 0) {
								linkText = linkUrl;
							}
							$('#msgArea').val( $('#msgArea').val() + '\n['+linkText+'](' + linkUrl + ')\n' );
							setTimeout(function() {
								// put focus at end of textarea
								var text = $("#msgArea").val();
								$("#msgArea").focus().val("").val(text);
							}, 100);
						}
					});
					$("#addHeader").click(function() {
						$('#msgArea').val( $('#msgArea').val() + '\n## ' );
						setTimeout(function() {
							// put focus at end of textarea
							var text = $("#msgArea").val();
							$("#msgArea").focus().val("").val(text);
						}, 80);
					});
					$("#addList").click(function() {
						$('#msgArea').val( $('#msgArea').val() + '\n*  ' );
						setTimeout(function() {
							// put focus at end of textarea
							var text = $("#msgArea").val();
							$("#msgArea").focus().val("").val(text);
						}, 80);
					});
					$("#markdownHelp").click(function() {
						var win = window.open('https://duckduckgo.com/?q=markdown+cheat+sheet&ia=answer&iax=1', '_blank');
						if (win) {
							//Browser has allowed it to be opened
							win.focus();
						} else {
							//Browser has blocked it
							alert('Visit: https://duckduckgo.com/?q=markdown+cheat+sheet&ia=answer&iax=1 for tips on using Markdown.');
						}
					});
      </script>
    </bodY>
  </html>`
}
