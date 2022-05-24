+++
title = "Choria Async Jobs"
weight = 5
+++

# Introduction

This is an Asynchronous Job Queue system that relies on NATS JetStream for storage and general job life cycle management. It is compatible with any NATS JetStream based system like a private hosted JetStream, Choria Streams or a commercial SaaS.

Each Task is stored in JetStream by a unique ID and Work Queue item is made referencing that Task. JetStream will handle dealing with scheduling, retries, acknowledgements and more of the Work Queue item. The stored Task will be updated during the lifecycle.

Multiple processes can process jobs concurrently, thus job processing is both horizontally and vertically scalable. Job handlers are implemented in Go with one process hosting one or many handlers. Other languages can implement Job Handlers using NATS Request-Reply services. Per process concurrency and overall per-queue concurrency controls exist.

## Synopsis

Tasks are published to Work Queues:

```go
// establish a connection to the EMAIL work queue using a NATS context
client, _ := asyncjobs.NewClient(asyncjobs.NatsConn(nc), asyncjobs.BindWorkQueue("EMAIL"))

// create a task with the type 'email:new' and body from newEmail()
task, _ := asyncjobs.NewTask("email:new", newEmail())

// store it in the Work Queue
client.EnqueueTask(ctx, task)
```

Tasks are processes by horizontally and vertically scalable processes. Typically, a Handler handles one type of Task. We have Prometheus
integration, concurrency and backoffs configured.

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
router.Handler("email:new", func(ctx context.Context, log asyncjobs.Logger, task *asyncjobs.Task) (interface{}, error) {
	log.Printf("Processing task %s", task.ID)

	// do work here using task.Payload

	return "sent", nil
})

client.Run(ctx, router)
```
