package main

import (
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/jcuga/golongpoll"
)

func main() {
	// Our chat server is just a longpoll/pub-sub server.
	manager, err := golongpoll.StartLongpoll(golongpoll.Options{})
	if err != nil {
		log.Fatalf("Failed to create chat longpoll manager: %q", err)
	}
	http.HandleFunc("/", Index)
	http.HandleFunc("/post", getChatPostClosure(manager))
	http.HandleFunc("/subscribe", manager.SubscriptionHandler)
	listenAddress := ":8080"
	log.Printf("Launching chat server on %s", listenAddress)
	http.ListenAndServe(listenAddress, nil)
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
	return func(w http.ResponseWriter, r *http.Request) {
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
		display_name := r.PostFormValue("display_name")
		message := r.PostFormValue("message")
		if len(strings.TrimSpace(topic)) == 0 || len(strings.TrimSpace(display_name)) == 0 ||
			len(strings.TrimSpace(message)) == 0 {
			http.Error(w, "Invalid request.  Blank topic, display_name, or message.", 400)
			return
		}
		chat := ChatPost{DisplayName: display_name, Message: message, Topic: topic}
		manager.Publish(topic, chat)
	}
}

func Index(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Invalid request method.", 405)
		return
	}
	topic := r.URL.Query().Get("topic")
	t := template.New("chat_homepage")
	t, _ = t.Parse(getIndexTemplateString())
	templateData := struct{ Topic string }{topic}
	t.Execute(w, templateData)
}

func getIndexTemplateString() string {
  // TODO: show all chats in list, include link to topic
  // TODO: go back and add listenaddr to template data (make listen addr a const)
  // TODO: create reserved topic for all chats
  // TODO: add all chat topic const to template data
  // TODO: conditionally subscribe to all or just current
  // TODO: links to filter by topic
  // TODO: show latest chat first
  // TODO: box up top for name, chat, topic (hidden vs text field depending on
  // if topic filter on.)
	return `<html>
    <head>
      <title>micro-chat</title>
      <script src="http://code.jquery.com/jquery-1.11.3.min.js"></script>
    </head>
    <body>
      <h1>MicroChat in Golang</h1>
      <h2>
      {{ if .Topic }}
        Showing chats about: {{ .Topic}} <a href="/">Show all chats.</a>
      {{ else }}
        Showing all chats
      {{ end }}</h2>
      <ul id="chats_list">
      </ul>
      <script>
          // for browsers that don't have console
          if(typeof window.console == 'undefined') { window.console = {log: function (msg) {} }; }

          // Start checking for any events that occurred within 30 minutes prior to page load
          // so you can switch pages to other users, and then come back and see
          // recent events:
          var yourActionsSinceTime = (new Date(Date.now() - 1800000)).getTime();

          // Let's subscribe to animal related events.
          var category = "farm";
          (function poll() {
              var timeout = 45;  // in seconds
              var optionalSince = "";
              if (sinceTime) {
                  optionalSince = "&since_time=" + sinceTime;
              }
              var pollUrl = "http://127.0.0.1:8081/basic/events?timeout=" + timeout + "&category=" + category + optionalSince;
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
                              $("#animal-events").append("<li>" + event.data + " at " + (new Date(event.timestamp).toLocaleTimeString()) +  "</li>")
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
