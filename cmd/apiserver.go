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

	http.HandleFunc("/ping", HelloHandler)

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

func HelloHandler(w http.ResponseWriter, _ *http.Request) {

	runner.Execute()

	nfo, err := runner.Client.StorageAdmin().QueueInfo("PING")
	if err != nil {
		log.Fatal(err)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(err.Error()))
	}

	fmt.Println(nfo)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(api.MakeQueueInfo(nfo)))
}
