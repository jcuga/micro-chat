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
			http.Error(w, "Invalid request.  Blank topic, display_name, or message.", 400)
			return
		}
		chat := ChatPost{DisplayName: display_name, Message: message, Topic: topic}
		manager.Publish(topic, chat)
		// show on the all-chats channel as well that shows on the homepage when you
		// haven't filtered to a specific topic.
		manager.Publish(ALL_CHATS, chat)
		// redirect to the chat page for the given topic
		http.Redirect(w, r, "/?topic="+topic+"&display_name="+display_name, http.StatusSeeOther)
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
    </head>
    <body>
			<form method="POST" action="/post">
				{{ if .Topic }}
				  <input type="hidden" name="topic" value="{{ .Topic }}">
				{{ else }}
				  <label for="topic">Topic:</label><br><input type="text" name="topic"><br>
				{{ end }}
				<label for="display_name">Post as</label><br/>
				<input type="text" name="display_name" value="{{if .DisplayName}}{{.DisplayName}}{{end}}">
				<br/>
				<label for="message">Message</label><br/>
				<textarea rows="2" cols="50" name="message"></textarea>
				<br>
				<input type="submit" value="post">
			</form>
			<hr>
			{{ if .Topic }}
        <h2>Showing chats about: {{ .Topic}}</h2>
				<a href="/">Show all chats.</a>
      {{ else }}
        <h2>Showing all chats</h2>
      {{ end }}
      <ul id="chats_list">
      </ul>
      <script>
          // for browsers that don't have console
          if(typeof window.console == 'undefined') { window.console = {log: function (msg) {} }; }

          // Start checking for any events that occurred within 24 hours minutes prior to page load
          // so we display recent chats:
          var sinceTime = (new Date(Date.now() - 86400000)).getTime();

          // subscribe to a specific topic or all chats
					var category = "{{ if .Topic }}{{ .Topic }}{{ else }}{{ .AllChats }}{{ end }}";

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
              $.ajax({ url: pollUrl,
                  success: function(data) {
                      if (data && data.events && data.events.length > 0) {
                          // got events, process them
                          // NOTE: these events are in chronological order (oldest first)
                          for (var i = 0; i < data.events.length; i++) {
                              // Display event
                              var event = data.events[i];
                              $("#chats_list").prepend(
																"<li>in <a href='/?topic=" + event.data.topic + "'>" + event.data.topic + "</a>" + "<br>" + event.data.message + "<br />by " + event.data.display_name + " at "  + (new Date(event.timestamp).toLocaleTimeString()) +  "</li>"
															)
                              // Update sinceTime to only request events that occurred after this one.
                              sinceTime = event.timestamp;
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
      </script>
    </bodY>
  </html>`
}
