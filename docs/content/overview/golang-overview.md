+++
title = "Golang Walkthrough"
toc = true
weight = 10
+++

This is a basic walkthrough of publishing Tasks and handling Tasks in Go. A more thorough guide will be written in time.

This is an introductory guide, we have extensive [Go reference documentation](https://pkg.go.dev/github.com/choria-io/asyncjobs).

This guide is known to work with Release 0.0.4

## Connecting to JetStream

A connection to a JetStream server is needed, you can either prepare a connecting yourself or pass in the name of a [NATS Context](https://docs.nats.io/using-nats/nats-tools/nats_cli#nats-contexts)

NATS have a plethora of connection methods, security approaches, TLS or non TLS and even supports Websockets - you can configure it any way you wish. See the [nats.go](https://github.com/nats-io/nats.go/) package for details.

First an example passing in a already prepared nats connection:

```go
nc, err := nats.Connect("localhost:4222")
panicIfErr(err)

client, err := asyncjobs.NewClient(asyncjobs.NatsConn(nc))
panicIfErr(err)
```

Or if you have a NATS Context called `AJC` you can use it:

```go
client, err := asyncjobs.NewClient(asyncjobs.NatsContext("AJC"))
panicIfErr(err)
```

In both cases a number of options can be supplied to log disconnections, reconnections and more.

## Configuring Queues

A Queue is where messages go, you can have many different, named, queues if you wish.  If you do not specify any Queue a default one is made called `DEFAULT`.

You might make different Queues to set different concurrency limits, different max attempts, maximum validity and more.

```go
queue := &asyncjobs.Queue{Name: "EMAIL", MaxRunTime: 60 * time.Minute, MaxTries: 50}

client, err := asyncjobs.NewClient(asyncjobs.NatsContext("EMAIL"), asyncjobs.WorkQueue(queue))
panicIfErr(err)
```

Here we attach to or create a new queue called `EMAIL` setting some specific options.  If the queue already exist we will just attach but not update configuration. You can prevent on-demand creation by setting `NoCreate: true`. See [go doc for details](https://pkg.go.dev/github.com/choria-io/asyncjobs@main#Queue).

## Creating and Enqueueing Tasks

A task can be anything you wish as long as it can serialize to JSON. Tasks have types like `email:new`, `email-new` or really anything you want, we'll see later how task types interact with the routing system.

Any number of producers can create tasks from any number of different processes.

First we have a simplistic helper to create a map that describes an email:

```go
func newEmail(to, subject, body string) any {
        return map[string]string{"to": to, "subject": subject, "body": body}
}
```

Now we can create a new email task and enqueue it:

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

## Consuming and Processing Tasks

Messages are consumed and handled by matching their type and from a specific Queue. Task processors can run concurrently across different processes and each processes can process a number of tasks concurrently. Per-process and per-Queue concurrency limits can be set.

We'll show a few more options than typical to get a feel for what's possible.

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

err = client.Run(ctx, routeR)
panicIfErr(err)
```

Here we registered one handler for `email:new` and a callback that will handle that task up to 10 at a time.

## Loading a task

Existing tasks can be loaded which will include their status and other details:

```go
client, err := asyncjobs.NewClient(asyncjobs.NatsContext("EMAIL"))
panicIfErr(err)

task, err := client.LoadTaskByID("24Y0rDk7kMHYHKwMSCxQZOocLH3")
panicIfErr(err)
```
