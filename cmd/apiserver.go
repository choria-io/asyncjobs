package main

import (
	"fmt"
	"log"
	"net/http"

	aj "github.com/choria-io/asyncjobs"
	"github.com/choria-io/asyncjobs/api"
)

var runner api.AJRunner

func main() {

	var err error

	http.HandleFunc("/queue", queueHandler)

	runner.Client, err = aj.NewClient(
		aj.NatsContext("AJC"),
		aj.BindWorkQueue("PING"),
		aj.ClientConcurrency(10),
	)
	if err != nil {
		log.Fatal("Failed to create an Asynjobs runner \n")
	}

	runner.Router = aj.NewTaskRouter()
	if runner.Router == nil {
		log.Fatal("Failed to create an Asynjobs router \n")
	}

	log.Println("Listening...")
	log.Fatal(http.ListenAndServe(":8088", nil))
}

func queueHandler(w http.ResponseWriter, r *http.Request) {

	runner.Execute()

	switch r.Method {
	case "GET":
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte("NYI - Not Yet Implemented"))
	case "POST":
		// Call ParseForm() to parse the raw query and update r.PostForm and r.Form.
		if err := r.ParseForm(); err != nil {
			fmt.Printf("ParseForm() err: %v", err)
			return
		}

		name := r.FormValue("name")
		fmt.Printf("Queue name = %s\n", name)

		if name != "" {
			nfo, err := runner.Client.StorageAdmin().QueueInfo(name)
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(err.Error()))
				fmt.Printf("Failed to read the queue %s \n", name)
			}

			w.WriteHeader(http.StatusOK)
			w.Write([]byte(api.MakeQueueInfo(nfo)))
		} else {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Queue name is empty"))
		}

	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}
