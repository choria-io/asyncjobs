![Choria Asynchronous Jos](https://choria.io/async-logo-horizontal.png)

## Overview

This is an Asynchronous Job Queue system that relies on NATS JetStream for storage and general job life cycle management.
It is compatible with any NATS JetStream based system like a private hosted JetStream, Choria Streams or a commercial SaaS.

Each Task is stored in JetStream by a unique ID and Work Queue item is made referencing that Task. JetStream will handle
dealing with scheduling, retries, acknowledgements and more of the Work Queue item.  The stored Task will be updated
during the lifecycle.

Multiple processes can process jobs concurrently, thus job processing is both horizontally and vertically scalable. Job
handlers are implemented in Go with one process hosting one or many handlers. Per process concurrency and overall per-queue
concurrency controls exist.

This package heavily inspired by [hibiken/asynq](https://github.com/hibiken/asynq/).

 * [Status](#status)
 * [Features](#features)
 * [Wiki](https://github.com/choria-io/asyncjobs/wiki)
 * [Community](https://github.com/choria-io/asyncjobs/discussions)
 * Examples
   * [Golang](https://github.com/choria-io/asyncjobs/wiki/Introductory-Golang-Walkthrough)
   * [CLI](https://github.com/choria-io/asyncjobs/wiki/Introductory-CLI-Walkthrough)
 
 [![Go Reference](https://pkg.go.dev/badge/github.com/choria-io/asyncjobs.svg)](https://pkg.go.dev/github.com/choria-io/asyncjobs)
 [![Go Report Card](https://goreportcard.com/badge/github.com/choria-io/asyncjobs)](https://goreportcard.com/report/github.com/choria-io/asyncjobs) 
 [![CodeQL](https://github.com/choria-io/asyncjobs/workflows/CodeQL/badge.svg)](https://github.com/choria-io/asyncjobs/actions/workflows/codeql.yaml) 
 [![Unit Tests](https://github.com/choria-io/asyncjobs/actions/workflows/test.yaml/badge.svg)](https://github.com/choria-io/asyncjobs/actions/workflows/test.yaml)

## Status

This is a brand-new project, under heavy development. Interfaces might change,
Structures might change, features might be removed if it's found to be a bad fit for the underlying storage.

Use with care.

## Synopsis

Tasks are published to Work Queues:

```go
// connect to NATS
nc,  := nats.Connect("localhost:4222")

// establish a connection to the EMAIL work queue
client, _ := asyncjobs.NewClient(asyncjobs.NatsConn(nc), asyncjobs.BindWorkQueue("EMAIL"))

// create a task with the type 'email:new' and body from newEmail()
task, _ := asyncjobs.NewTask("email:new", newEmail())

// store it in the Work Queue
client.EnqueueTask(ctx, task)
```

Tasks are processes by horizontally and vertically scalable. Typically, a Handler handles one type of Task. We have Prometheus
integration, concurrency and backoffs configured.

```go
// establish a connection to the EMAIL work queue using a NATS context, with concurrency, prometheus stats and backoff
client, _ := asyncjobs.NewClient(
	asyncjobs.NatsContext("EMAIL"), 
	asyncjobs.BindWorkQueue("EMAIL"),
	asyncjobs.ClientConcurrency(10),
	asyncjobs.PrometheusListenPort(8080),
	asyncjobs.RetryBackoffPolicy(asyncjobs.RetryLinearTenMinutes))

router := asyncjobs.NewTaskRouter()
router.Handler("email:new", func(ctx context.Context, task *asyncjobs.Task) (interface{}, error) {
	log.Printf("Processing task %s", task.ID)

	// do work here using task.Payload

	return "sent", nil
})

client.Run(ctx, router)
```

See our [Wiki](https://github.com/choria-io/asyncjobs/wiki) for a deep dive into the use cases, architecture, abilities and more.

## Requirements

NATS 2.7.2 with JetStream enabled.

## Features

See the [Feature List](https://github.com/choria-io/asyncjobs/wiki/Features) page for a full feature break down.
