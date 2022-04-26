package main

import (
	"context"
	"log"
	"time"

	aj "github.com/choria-io/asyncjobs"
)

const (
	topic = "aj:pingpong"
)

func main() {

	// Input queue
	pingClient, err := aj.NewClient(
		aj.NatsContext("AJC"),
		aj.BindWorkQueue("PING"),
		aj.ClientConcurrency(10),
		// aj.PrometheusListenPort(8089),
		aj.RetryBackoffPolicy(aj.RetryLinearOneMinute))

	if err != nil {
		log.Fatal(err)
	}

	// Output queue
	pongClient, err := aj.NewClient(
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
	err = router.HandleFunc(topic, func(_ context.Context, _ aj.Logger, t *aj.Task) (interface{}, error) {
		log.Printf("*** Processing PING task with ID: %s", t.ID)

		cmd := Command{
			time: time.Now(),
		}

		task, err := aj.NewTask("sdk::ec2", cmd, aj.TaskDeadline(time.Now().Add(time.Hour)))
		if err != nil {
			return nil, err
		}

		log.Println("*** PING: Sending PONG task")
		err = pongClient.EnqueueTask(context.Background(), task)
		if err != nil {
			return nil, err
		}

		return "success", nil
	})

	// Starts handling registered tasks, blocks until canceled
	err = pingClient.Run(context.Background(), router)

}

type Command struct {
	time time.Time
}
