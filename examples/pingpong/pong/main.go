package main

import (
	"context"
	"log"

	aj "github.com/choria-io/asyncjobs"
)

func main() {

	// Input queue
	pingClient, err := aj.NewClient(
		aj.NatsContext("AJC"),
		aj.BindWorkQueue("PONG"),
		aj.ClientConcurrency(10),
		// aj.PrometheusListenPort(8089),
		aj.RetryBackoffPolicy(aj.RetryLinearOneMinute))

	if err != nil {
		log.Fatal(err)
	}

	router := aj.NewTaskRouter()

	// Create a handler - a single handler must be implemented for each topic
	err = router.HandleFunc("sdk::ec2", func(_ context.Context, _ aj.Logger, t *aj.Task) (interface{}, error) {
		log.Printf("*** Processing PONG task with ID: %s", t.ID)

		return "success", nil
	})

	// Starts handling registered tasks, blocks until canceled
	err = pingClient.Run(context.Background(), router)
}
