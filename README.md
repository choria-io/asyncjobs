![Choria Asynchronous Jos](https://choria.io/async-logo-horizontal.png)

## Overview

This is an Asynchronous Job Queue system that relies on NATS JetStream for storage and general job life cycle management.
It is compatible with any NATS JetStream based system like a private hosted JetStream, Choria Streams or a commercial SaaS.

Each Task is stored in JetStream by a unique ID and Work Queue item is made referencing that Task. JetStream will handle
dealing with scheduling, retries, acknowledgements and more of the Work Queue item.  The stored Task will be updated
during the lifecycle.

Multiple processes can process jobs concurrently, thus job processing is both horizontally and vertically scalable. Job
handlers are implemented in Go with one process hosting one or many handlers. Other languages can implement Job Handlers using
NATS Request-Reply services. Per process concurrency and overall per-queue concurrency controls exist.

This package heavily inspired by [hibiken/asynq](https://github.com/hibiken/asynq/).

 * [Documentation](https://choria-io.github.io/asyncjobs/)
 * [Community](https://github.com/choria-io/asyncjobs/discussions)
 * Examples
   * [Golang](https://choria-io.github.io/asyncjobs/overview/golang-overview/)
   * [CLI](https://choria-io.github.io/asyncjobs/overview/cli-overview/)
 
 [![Go Reference](https://pkg.go.dev/badge/github.com/choria-io/asyncjobs.svg)](https://pkg.go.dev/github.com/choria-io/asyncjobs)
 [![Go Report Card](https://goreportcard.com/badge/github.com/choria-io/asyncjobs)](https://goreportcard.com/report/github.com/choria-io/asyncjobs) 
 [![CodeQL](https://github.com/choria-io/asyncjobs/workflows/CodeQL/badge.svg)](https://github.com/choria-io/asyncjobs/actions/workflows/codeql.yaml) 
 [![Unit Tests](https://github.com/choria-io/asyncjobs/actions/workflows/test.yaml/badge.svg)](https://github.com/choria-io/asyncjobs/actions/workflows/test.yaml)

## Status

This is a brand-new project, under heavy development. The core Task handling is in good shape and reasonably stable. Task Scheduler is still subject to some change.

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
router.Handler("email:new", func(ctx context.Context, log asyncjobs.Logger, task *asyncjobs.Task) (any, error) {
	log.Printf("Processing task %s", task.ID)

	// do work here using task.Payload

	return "sent", nil
})

client.Run(ctx, router)
```

See our [documentation](https://choria-io.github.io/asyncjobs/) for a deep dive into the use cases, architecture, abilities and more.

## HTTP API

Non-Go services and operators can use asyncjobs over HTTP. The CLI ships an `ajc server run` subcommand that exposes the OpenAPI 3.0 surface defined in [`api/openapi.yaml`](api/openapi.yaml).

```
# create the queue the API will accept tasks for
ajc queue add EMAIL --run-time 1h

# launch the server on loopback
ajc server run --queue EMAIL
```

```
# enqueue a task
curl -sf -X POST http://127.0.0.1:8080/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"type":"email:new","payload":{"to":"user@example.net"}}'
```

The server performs no authentication. For non-loopback deployments, front it with a reverse proxy that terminates authn, or configure mTLS via `--tls-cert`/`--tls-key`/`--tls-client-ca`. The full surface — deployment recipes, endpoint catalog, error envelope, payload encoding quirks, and client generation guidance — lives in the [HTTP API documentation](https://choria-io.github.io/asyncjobs/reference/http-api/).

## Requirements

NATS 2.8.0 or newer with JetStream enabled.

## Features

See the [Feature List](https://choria-io.github.io/asyncjobs/overview/features/) page for a full feature break down.
