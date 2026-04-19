# Golang Walkthrough

This walkthrough covers publishing tasks and handling them in Go. A more thorough guide is planned. Complete [Go reference documentation](https://pkg.go.dev/github.com/choria-io/asyncjobs) is available on pkg.go.dev.

The guide is known to work with Release 0.0.4.

## Connecting to JetStream

A connection to a JetStream server is required. Either a prepared NATS connection or the name of a [NATS Context](https://docs.nats.io/using-nats/nats-tools/nats_cli#nats-contexts) can be passed in.

NATS supports many connection methods, security approaches, TLS or non-TLS, and websockets. See the [nats.go](https://github.com/nats-io/nats.go/) package for details.

Passing in a prepared NATS connection:

```go
nc, err := nats.Connect("localhost:4222")
panicIfErr(err)

client, err := asyncjobs.NewClient(asyncjobs.NatsConn(nc))
panicIfErr(err)
```

Using a NATS Context called `AJC`:

```go
client, err := asyncjobs.NewClient(asyncjobs.NatsContext("AJC"))
panicIfErr(err)
```

In both cases, additional options can log disconnections, reconnections, and more.

## Configuring queues

A queue holds messages for processing. Many named queues can coexist. Without an explicit queue, a default called `DEFAULT` is used.

Different queues support different concurrency limits, maximum attempts, and validity periods.

```go
queue := &asyncjobs.Queue{Name: "EMAIL", MaxRunTime: 60 * time.Minute, MaxTries: 50}

client, err := asyncjobs.NewClient(asyncjobs.NatsContext("EMAIL"), asyncjobs.WorkQueue(queue))
panicIfErr(err)
```

The call attaches to or creates a queue called `EMAIL` with specific options. If the queue exists, the client attaches without updating the configuration. Setting `NoCreate: true` prevents on-demand creation. See the [Queue reference](https://pkg.go.dev/github.com/choria-io/asyncjobs@main#Queue) for details.

## Creating and enqueueing tasks

A task can carry any payload that serializes to JSON. Task types such as `email:new` or `email-new` drive routing to handlers.

Any number of producers can create tasks from any number of processes.

A helper creates a map describing an email:

```go
func newEmail(to, subject, body string) any {
        return map[string]string{"to": to, "subject": subject, "body": body}
}
```

A new email task can then be created and enqueued:

```go
client, err := asyncjobs.NewClient(
        asyncjobs.NatsContext("EMAIL"), 
        asyncjobs.WorkQueue(&asyncjobs.Queue{Name: "EMAIL", NoCreate: true}))
panicIfErr(err)

email := newEmail("user@example.net", "Test Subject", "Test Body")

task, err := asyncjobs.NewTask("email:new", email)
panicIfErr(err)

err = client.EnqueueTask(context.Background(), task)
panicIfErr(err)
```

The task is sent to the store and placed in the `EMAIL` work queue for processing.

## Consuming and processing tasks

Messages are consumed and handled by matching their type on a specific queue. Task processors can run concurrently across processes, and each process can handle multiple tasks concurrently. Per-process and per-queue concurrency limits are configurable.

The following example uses more options than typical:

```go
client, err := asyncjobs.NewClient(
        asyncjobs.NatsContext("EMAIL"),
        // 10 Tasks handled by this process concurrently
        asyncjobs.ClientConcurrency(10),
        // Prometheus stats on 0.0.0.0:8080/metrics
        asyncjobs.PrometheusListenPort(8080), 
        // Logs using an already-prepared logger
        asyncjobs.CustomLogger(log),
        // Schedules retries on a jittering backoff between 1 and 10 minutes
        asyncjobs.RetryBackoffPolicy(asyncjobs.RetryLinearTenMinutes),
        // Connects to a queue that should already exist
        asyncjobs.BindWorkQueue("EMAIL"))
panicIfErr(err)

router := asyncjobs.NewTaskRouter()
err = router.Handler("email:new", func(ctx context.Context, log asyncjobs.Logger, task *asyncjobs.Task) (any, error) {
        log.Printf("Processing task %s", task.ID)

        // do work here using task.Payload

        return "sent", nil
})
panicIfErr(err)

err = client.Run(ctx, router)
panicIfErr(err)
```

The example registers one handler for `email:new` with a callback that handles up to 10 tasks at a time.

## Loading a task

Existing tasks can be loaded, including their status and other details:

```go
client, err := asyncjobs.NewClient(asyncjobs.NatsContext("EMAIL"))
panicIfErr(err)

task, err := client.LoadTaskByID("24Y0rDk7kMHYHKwMSCxQZOocLH3")
panicIfErr(err)
```
