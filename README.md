# Choria Async Jobs

## Overview

This is an Asynchronous Job Queue system that relies on NATS JetStream for storage and general job life cycle management.
It is compatible with any NATS JetStream based system like a private hosted JetStream, Choria Streams or a commercial SaaS.

Each Task is stored in JetStream by a unique ID and Work Queue item is made referencing that Task. JetStream will handle
dealing with scheduling, retries, acknowledgements and more of the Work Queue item.  The stored Task will be updated
during the lifecycle.

A single process can handle different types of task by means of different named Queues that can have priority and are
polled in a priority-weighted manner for work.

Multiple processes can process jobs concurrently, thus job processing is both horizontally and vertically scalable.

## Status

This is a brand-new project, under heavy development and relies on unreleased behaviors in JetStream. Interfaces might change,
Structures might change, features might be removed if it's found to be a bad fit for the underlying storage.

Use with care.

## Features

This feature list is incomplete, at present the focus is on determining what will work well for the particular patterns
JetStream enables, so there might be some churn in the feature set here.

### Tasks

* Task definitions stored post-processing, with various retention policies client opt `TaskRetention()`
* Task DLQ for failed or expired task definitions, with various retention policies (required more thought)
* Task deduplication
* Deadline per task - after this time the task will not be processed
* Max tries per task, capped to the queue tries
* Task state tracked during it's lifetime
* [K-Sortable](https://github.com/segmentio/ksuid) Task GUIDs

### Queues

* Weighted Priority Queues
* Queues with caps on queued items and different queue-full behaviors (DiscardOld on queue, sets task to TaskStateQueueError)
* Default or user supplied queue definitions

### Processing

* Retries of failed tasks with backoff schedules configurable using `RetryBackoffPolicy()`
* Parallel processing of tasks, horizontally or vertically scaled. Run time adjustable upper boundary on a per-queue basis (queue.MaxConcurrent)
* Worker crashes does not impact the work queue
* Handler interface with middleware (planned, mux is super minimal and needs a rego)
* Statistics via Prometheus using `PrometheusListenPort()`
* Real time lifecycle events (planned)

### Storage

* Replicated storage using RAFT protocol, disk based or memory based using `MemoryStorage()`

### Misc

* Supports NATS Contexts for connection configuration
* Task Scheduling via external Scheduler
* Supports custom loggers, defaulting to go internal `log`

## Planned Features

* REST Service for enqueuing
* Explore options for other languages, for example delegating the execution of a task over nats core request-reply
* A CLI that can configure some aspects of queues like max concurrency etc, view tasks, view events etc
* A CLI to enqueue jobs
* A CLI that can listen on queues and execute local scripts
* A scheduler service that creates tasks on a schedule

## Example

Tasks are enqueued using the client, here we create a task with a map as payload, we set a deadline for 1 hour to 
finish the task and enqueue it.

The task is of type `example` and is placed in the `TEST` queue. This queue allows for up to 100 processing attempts
with each attempt having up to 1 hour to complete. 

```go
queue := &Queue{
	Name: "TEST",
	MaxRunTime: 60*time.Minute,
	MaxTries: 100,
	MaxConcurrent: 100,
	Priority: 10,
}

client, _ := NewClient(NatsContext("WQ"), WorkQueues(queue))

payload := map[string]string{"hello": "world"}
task, _ := NewTask("example", payload, TaskDeadline(time.Now().Add(time.Hour)))
_ = client.EnqueueTask(ctx, "TEST", task)
```

One or many job processors can be started to consume the work in the Queues:

```go
queue := &Queue{
	Name: "TEST",
	MaxRunTime: 60*time.Minute,
	MaxTries: 100,
	MaxConcurrent: 100,
	Priority: 10,
}

client, _ := NewClient(NatsContext("WQ"), WorkQueues(queue))

router := NewTaskRouter()
router.HandleFunc("example", func(ctx context.Context, t *Task) ([]byte, error) {
	log.Printf("Processing task %s", t.ID)
	
	return []byte("done"), nil
})

client.Run(ctx, router)
```

Here we create a handler for `example` type tasks that just always finish it. Tasks can come from any of the defined
queues, one queue can hold many types of task.

Tasks can be loaded via the client:

```go
client, _ := NewClient(NatsContext("WQ"))
task, _ := client.LoadTaskByID("24ErgVol4ZjpoQ8FAima9R2jEHB")
```

A completed task will look like this:

```json
{
  "id": "24ErgVol4ZjpoQ8FAima9R2jEHB",
  "type": "example",
  "queue": "TEST",
  "payload": "eyJoZWxsbyI6IndvcmxkIn0=",
  "result": {
    "payload": "ZG9uZQ==",
    "completed": "2022-01-26T14:46:09.427182Z"
  },
  "state": "complete",
  "created": "2022-01-26T14:46:09.232015Z",
  "tried": "2022-01-26T14:46:09.427182Z",
  "tries": 1
}
```
