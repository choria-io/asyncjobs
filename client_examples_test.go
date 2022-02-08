package asyncjobs

import (
	"context"
	"fmt"
	"log"
	"time"
)

func newEmail(to, subject, body string) map[string]interface{} {
	return map[string]interface{}{
		"to":      to,
		"subject": subject,
		"body":    body,
	}
}

func panicIfErr(err error) {
	if err != nil {
		panic(err)
	}
}

func ExampleClient_producer() {
	queue := &Queue{
		Name:          "P100",
		MaxRunTime:    60 * time.Minute,
		MaxConcurrent: 20,
		MaxTries:      100,
	}

	email := newEmail("user@example.net", "Test Subject", "Test Body")

	// Creates a new task that has a deadline for processing 1 hour from now
	task, err := NewTask("email:send", email, TaskDeadline(time.Now().Add(time.Hour)))
	panicIfErr(err)

	// Uses the NATS CLI context WQ for connection details, will create the queue if
	// it does not already exist
	client, err := NewClient(NatsContext("WQ"), WorkQueue(queue))
	panicIfErr(err)

	// Adds the task to the queue called P100
	err = client.EnqueueTask(context.Background(), task)
	panicIfErr(err)
}

func ExampleNewTask_with_deadline() {
	email := newEmail("user@example.net", "Test Subject", "Test Body")

	// Creates a new task that has a deadline for processing 1 hour from now
	task, err := NewTask("email:send", email, TaskDeadline(time.Now().Add(time.Hour)))
	if err != nil {
		panic(fmt.Sprintf("Could not create task: %v", err))
	}

	fmt.Printf("Task ID: %s\n", task.ID)
}

func ExampleClient_consumer() {
	queue := &Queue{
		Name:          "P100",
		MaxRunTime:    60 * time.Minute,
		MaxConcurrent: 20,
		MaxTries:      100,
	}

	// Uses the NATS CLI context WQ for connection details, will create the queue if
	// it does not already exist
	client, err := NewClient(NatsContext("WQ"), WorkQueue(queue), RetryBackoffPolicy(RetryLinearOneHour))
	panicIfErr(err)

	router := NewTaskRouter()
	err = router.HandleFunc("email:send", func(_ context.Context, _ Logger, t *Task) (interface{}, error) {
		log.Printf("Processing task: %s", t.ID)

		// handle task.Payload which is a JSON encoded email

		// task record will be updated with this payload result
		return "success", nil
	})
	panicIfErr(err)

	// Starts handling registered tasks, blocks until canceled
	err = client.Run(context.Background(), router)
	panicIfErr(err)
}

func ExampleClient_LoadTaskByID() {
	client, err := NewClient(NatsContext("WQ"))
	panicIfErr(err)

	task, err := client.LoadTaskByID("24ErgVol4ZjpoQ8FAima9R2jEHB")
	panicIfErr(err)

	fmt.Printf("Loaded task %s in state %s", task.ID, task.State)
}
