package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/choria-io/asyncjobs/api"
)

var client *api.AJRunner

func main() {

	http.HandleFunc("/ping", HelloHandler)

	client = new(api.AJRunner)
	if client == nil {
		log.Fatal("Failed to create an Asynjobs client \n")
	}

	log.Println("Listening...")
	log.Fatal(http.ListenAndServe(":8088", nil))
}

func HelloHandler(w http.ResponseWriter, _ *http.Request) {

	fmt.Fprintf(w, "Hello, there\n")
	client.Execute()
}
