package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/jcuga/golongpoll"
	"github.com/julienschmidt/httprouter"
)

func main() {
	// Our chat server is just a longpoll server.
	manager, err := golongpoll.StartLongpoll(golongpoll.Options{
		LoggingEnabled:                 false,
		MaxLongpollTimeoutSeconds:      120,
		MaxEventBufferSize:             1000,             // each topic will buffer last 1000 posts
		EventTimeToLiveSeconds:         golongpoll.FOREVER, // posts's stick around as long as there's room in the buffer
		DeleteEventAfterFirstRetrieval: false,            // pub-sub, so don't delete posts after first retrieval
	})
	if err != nil {
		log.Fatalf("Failed to create chat longpoll manager: %q", err)
	}
	// Expose chat via web:
	router := httprouter.New()
	router.GET("/", Index)
  // TODO: create topic page that publishes fact topic created and redir to topic page
  router.GET("/topic/:topic", ChatTopicPage)       // TODO: scrub data that gets posted
	router.GET("/post", getChatPostClosure(manager)) // TODO: scrub data that gets posted
	router.GET("/subscribe", wrapSubscriptionHandler(manager.SubscriptionHandler))
	// TODO: for topic page, if topic does not exist, ask to create (just does a post of "starting topic")
	listenAddress := ":8080"
	log.Printf("Launching chat server on %s", listenAddress)
	log.Fatal(http.ListenAndServe(listenAddress, router))
}

type ChatPost struct {
	DisplayName string `json:"display_name"`
	Message     string `json:"message"`
  Topic       string `json:"topic"`
  // TODO: ip address (forwarded-for addr too)
}

func Index(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if r.Method != "GET" {
		http.Error(w, "Invalid request method.", 405)
		return
	}
	// TODO: get list of topics and possibly some sort of activity metric
	// also have a create topic form that post and redir to topic room
	fmt.Fprint(w, "Welcome! todo show current topics and possibly stats about?\n")
}

func ChatTopicPage(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	if r.Method != "GET" {
		http.Error(w, "Invalid request method.", 405)
		return
	}
	// TODO: load html that allows for pub and sub of chat by this topic
	fmt.Fprintf(w, "Topic: %s\n", ps.ByName("topic"))
}

// Show chats by a given display name
// NOTE this can be from anyone who choses to use that alias
func AliasHistoryPage(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
  if r.Method != "GET" {
    http.Error(w, "Invalid request method.", 405)
    return
  }
  // TODO: load html that allows for pub and sub of chat by this topic
  fmt.Fprintf(w, "Posts by Alias: %s\n", ps.ByName("display_name"))
}

// httprouter wants its own specific handler type, so wrap ours for compatibility:
func wrapSubscriptionHandler(longpollSubHandler func(http.ResponseWriter, *http.Request)) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		longpollSubHandler(w, r)
	}
}

// Create a closure that contains a ref to our longpoll manager so we can
// call Publish() from within httprouter.Handler.
// NOTE: the manager is safe to call this way because it relies on channels
func getChatPostClosure(manager *golongpoll.LongpollManager) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		// TODO: manager.Publish(...)
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
		if !isValidTopic(topic) || !isValidDisplayName(display_name) || !isValidMessage(message) {
			http.Error(w, "Invalid request.  Either topic, display_name, or message was invalid.", 400)
			return
		}
		chat := ChatPost{DisplayName: display_name, Message: message, Topic: topic}
		// Publish on given chat room
		manager.Publish(topic, chat)
	}
}

func isValidTopic(topic string) bool {
	t := strings.TrimSpace(topic)
	if len(t) == 0 {
		return false

	// TODO: enforce more via regexp here
	return true
}

func isValidDisplayName(displayName string) bool {
	d := strings.TrimSpace(displayName)
	if len(d) == 0 {
		return false
	}
	// TODO: enforce more via regexp here
	return true
}

func isValidMessage(message string) bool {
	m := strings.TrimSpace(message)
	if len(m) == 0 {
		return false
	}
	// TODO: enforce more via regexp here
	return true
}

// TODO: func to scrub msg content to exclude links, scripts
