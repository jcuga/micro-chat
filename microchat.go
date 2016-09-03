package main

import (
	"flag"
	"github.com/jcuga/golongpoll"
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
	flag.Parse()

	// Our chat server is just a longpoll/pub-sub server.
	manager, err := golongpoll.StartLongpoll(golongpoll.Options{})
	if err != nil {
		log.Fatalf("Failed to create chat longpoll manager: %q", err)
	}
	http.HandleFunc("/", Index)
	http.HandleFunc("/post", getChatPostClosure(manager))
	http.HandleFunc("/subscribe", manager.SubscriptionHandler)
	log.Printf("Launching chat server on %s", *listenAddress)
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
		topic = truncateInput(topic, 48)
		display_name = truncateInput(display_name, 24)
		message = truncateInput(message, 256)
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

func Index(w http.ResponseWriter, r *http.Request) {
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
		Topic       string
		DisplayName string
		AllChats    string
	}{topic, displayName, ALL_CHATS}
	t.Execute(w, templateData)
}

func normalizeTopic(topic string, reg *regexp.Regexp) string {
	norm := reg.ReplaceAllString(topic, "-")
	norm = strings.ToLower(strings.Trim(norm, "-"))
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
      <script src="http://code.jquery.com/jquery-1.11.3.min.js"></script>
			<script src="https://cdnjs.cloudflare.com/ajax/libs/jquery-timeago/1.5.3/jquery.timeago.min.js"></script>
    </head>
    <body>
			<form id="chatForm" method="POST" action="/post">
				{{ if .Topic }}
				  <input type="hidden" id="topic" name="topic" value="{{ .Topic }}">
				{{ else }}
				  <label for="topic">Topic:</label><input type="text" maxlength="48" id="topic" name="topic">
				{{ end }}
				{{ if .DisplayName }}
				<input id="displayName" type="hidden" name="display_name" value="{{.DisplayName}}">
				{{ else }}
				<label id="nameLbl" for="display_name">Post as</label>
				<input id="displayName" type="text" maxlength="24" name="display_name" value="">
				<label for="message">Message</label>
				{{ end }}
				<textarea id="msgArea" rows="2" cols="50" name="message" maxlength="256"></textarea>
				{{ if .Topic }}
				  <!-- dynamic page instead of form post/redirect -->
					<button id="chat-btn" type="button">Post</button>
				{{ else }}
					<input id="chat-submit" type="submit" value="post">
				{{ end }}
				<div id="feedback"></div>
			</form>
			{{ if .Topic }}
        <h2 id="chat-topic-hdr">Chat topic: {{ .Topic}}</h2>
				<a href="/">Select other topic.</a>
      {{ else }}
        <h2 id="chat-topic-hdr">Showing all chats</h2>
      {{ end }}
      <div id="chats_list">
      </div>
			<div id="recent_topics">
				<h2 id="recent-topic-hdr">Recent topics</h2>
				<div id="recent_topics_list">
					<span class="nothing-yet">No topics yet.</span>
				</div>
      </div>
			<div id="popular_topics">
				<h2 id="popular-topic-hdr">Popular topics</h2>
				<div id="popular_topics_list">
					<span class="nothing-yet">No topics yet.</span>
				</div>
      </div>
      <script>
          // for browsers that don't have console
          if(typeof window.console == 'undefined') { window.console = {log: function (msg) {} }; }

          // Start checking for any events that occurred within 24 hours minutes prior to page load
          // so we display recent chats:
          var sinceTime = (new Date(Date.now() - 86400000)).getTime();  // TODO: template out this value

          // subscribe to a specific topic or all chats
					var category = "{{ if .Topic }}{{ .Topic }}{{ else }}{{ .AllChats }}{{ end }}";

					// for current page of chats--could be either specific category or all
					// chats
          (function poll() {
              var timeout = 45;  // in seconds
              var optionalSince = "";
              if (sinceTime) {
                  optionalSince = "&since_time=" + sinceTime;
              }
              var pollUrl = "/subscribe?timeout=" + timeout + "&category=" + category + optionalSince;
              // how long to wait before starting next longpoll request in each case:
              var successDelay = 10;  // 10 ms
              var errorDelay = 3000;  // 3 sec
							var maxChats = 4; // limit max number of chats displayed
              $.ajax({ url: pollUrl,
                  success: function(data) {
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
																topicPart = "<div class=\"topic\"><a href='/?topic=" + event.data.topic + "'>" + event.data.topic + "</a></div>"
															}
															$("#chats_list").prepend(
																	"<div class=\"chat\">" + topicPart + "<div class=\"msg\">" + event.data.message + "</div><div class=\"displayName\">" + event.data.display_name + "</div><div class=\"postTime\">"  + timestamp +  "</div></div>"
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
              var timeout = 45;  // in seconds
							// always fetch all chats during last N seconds
							// we don't update subsequent calls to timestamp of most
							// recent event because we're always fetching list of
							// recent, and not only ones since last call...
							var topicSinceTime = (new Date(Date.now() - 86400000)).getTime();  // TODO: template out this val
              var topicsSince = "&since_time=" + topicSinceTime;
              var pollUrl = "/subscribe?timeout=" + timeout + "&category=" + {{ .AllChats }} + topicsSince;
              // how long to wait before starting next longpoll request in each case:
							// these are spread out more than regular chat poll since this is
							// just show show pretty features like recent topics/popular topics
            	var successDelay = 10000;  // 10 sec  // TODO: template out?
              var errorDelay = 30000;  // 30 sec
							// number of topics in our Top Recent/Top Active iists
							var maxNumTopics = 3;  // TODO: template value?
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
															// event time stamps. we always fetch all chats within last N seconds.  // TODO: template time here
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
															var chatHtml = "<div class=\"chat\"><div class=\"msg\">" + event.data.message + "</div><div class=\"displayName\">" + event.data.display_name + "</div><div class=\"postTime\">"  + timestamp +  "</div></div>"
															$("#recent_topics_list").append("<div class=\"topic-item\"><a href=\"/?topic=" + sortableTopicTimes[i][0] + "\">" + sortableTopicTimes[i][0]  + "</a>" + chatHtml + "</div>");
														}
													}
													if (sortableTopicCounts.length > 0) {
														$("#popular_topics_list").empty();
														for (var i = 0; i < sortableTopicCounts.length && i < maxNumTopics; i++) {
															var event = sortableTopicCounts[i][1][1];
															var msgDate = new Date(event.timestamp);
															var timestamp = "<time class=\"timeago\" datetime=\"" + msgDate.toISOString() + "\">"+msgDate.toLocaleTimeString()+"</time>";
															var chatHtml = "<div class=\"chat\"><div class=\"msg\">" + event.data.message + "</div><div class=\"displayName\">" + event.data.display_name + "</div><div class=\"postTime\">"  + timestamp +  "</div></div>"

															$("#popular_topics_list").append("<div class=\"topic-item\"><a href=\"/?topic=" + sortableTopicCounts[i][0]  + "\">" + sortableTopicCounts[i][0]  + "</a> (" + sortableTopicCounts[i][1][0] + ")" + chatHtml + "</div>");
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
								$("#displayName").hide();
								$("#nameLbl").hide();
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
					    if (event.which == 13) {
					        event.preventDefault();
					        $("#chat-submit").click();
									$("#chat-btn").click();
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

      </script>
    </bodY>
  </html>`
}
