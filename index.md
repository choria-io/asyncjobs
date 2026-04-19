# Choria Async Jobs

Choria Async Jobs is an asynchronous job queue system backed by NATS JetStream. It works with any JetStream-compatible system, including self-hosted JetStream, Choria Streams, and commercial SaaS offerings.

Each task is stored in JetStream under a unique ID, with a work queue item referencing that task. JetStream handles scheduling, retries, and acknowledgements for the work queue item. The stored task is updated throughout its lifecycle.

Multiple processes can process jobs concurrently, giving horizontal and vertical scalability. Job handlers are written in Go, with one process hosting one or many handlers. Other languages can implement handlers through NATS Request-Reply services. Per-process and per-queue concurrency controls are available.

## Synopsis

Tasks are published to work queues:

```go
// establish a connection to the EMAIL work queue using a NATS context
client, _ := asyncjobs.NewClient(asyncjobs.NatsConn(nc), asyncjobs.BindWorkQueue("EMAIL"))

// create a task with the type 'email:new' and body from newEmail()
task, _ := asyncjobs.NewTask("email:new", newEmail())

// store it in the Work Queue
client.EnqueueTask(ctx, task)
```

Tasks are processed by horizontally and vertically scalable workers. A handler typically handles one type of task. Prometheus integration, concurrency limits, and retry backoff are configurable.

```go
// establish a connection to the EMAIL work queue using a 
// NATS context, with concurrency, prometheus stats and backoff
client, _ := asyncjobs.NewClient(
	asyncjobs.NatsContext("EMAIL"), 
	asyncjobs.BindWorkQueue("EMAIL"),
	asyncjobs.ClientConcurrency(10),
	asyncjobs.PrometheusListenPort(8080),
	asyncjobs.RetryBackoffPolicy(asyncjobs.RetryLinearTenMinutes))

router := asyncjobs.NewTaskRouter()
router.Handler("email:new", func(ctx context.Context, log asyncjobs.Logger, task *asyncjobs.Task) (any, error) {
	log.Printf("Processing task %s", task.ID)

	// do work here using task.Payload

	return "sent", nil
})

client.Run(ctx, router)
```
