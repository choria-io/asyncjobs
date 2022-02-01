![Choria Asynchronous Jos](https://choria.io/async-logo-horizontal.png)

## Overview

This is an Asynchronous Job Queue system that relies on NATS JetStream for storage and general job life cycle management.
It is compatible with any NATS JetStream based system like a private hosted JetStream, Choria Streams or a commercial SaaS.

Each Task is stored in JetStream by a unique ID and Work Queue item is made referencing that Task. JetStream will handle
dealing with scheduling, retries, acknowledgements and more of the Work Queue item.  The stored Task will be updated
during the lifecycle.

Different types of task can be stored in one Queue, a single process can attach to a single Queue.

Multiple processes can process jobs concurrently, thus job processing is both horizontally and vertically scalable.

This package heavily inspired by [hibiken/asynq](https://github.com/hibiken/asynq/).

 * [Status](#status)
 * [Features](#features)
 * [Examples](https://github.com/choria-io/asyncjobs/blob/main/client_examples_test.go)
 * [![Go Reference](https://pkg.go.dev/badge/github.com/choria-io/asyncjobs.svg)](https://pkg.go.dev/github.com/choria-io/asyncjobs)

## Status

This is a brand-new project, under heavy development and relies on unreleased behaviors in JetStream. Interfaces might change,
Structures might change, features might be removed if it's found to be a bad fit for the underlying storage.

Use with care.

## Features

This feature list is incomplete, at present the focus is on determining what will work well for the particular patterns
JetStream enables, so there might be some churn in the feature set here.

### Tasks

* Task definitions stored post-processing, with various retention policies
* Task deduplication
* Deadline per task - after this time the task will not be processed
* Max tries per task, capped to the queue tries
* Task state tracked during it's lifetime
* [K-Sortable](https://github.com/segmentio/ksuid) Task GUIDs

### Queues

* Queues can store different types of task
* Queues with caps on queued items and different queue-full behaviors
* Default or user supplied queue definitions
* Queue per client, many clients per queue

### Processing

* Retries of failed tasks with backoff schedules configurable using `RetryBackoffPolicy()`. Handler opt-in early termination.
* Parallel processing of tasks, horizontally or vertically scaled. Run time adjustable upper boundary on a per-queue basis
* Worker crashes does not impact the work queue
* Handler interface with task router to select appropriate handler by task type with wildcard matches
* Statistics via Prometheus

### Storage

* Replicated storage using RAFT protocol within JetStream Streams, disk based or memory based

### Misc

* Supports NATS Contexts for connection configuration
* Supports custom loggers, defaulting to go internal `log`

### Command Line

* Various info and state requests
* Configure aspects of Task and Queue storage
* Watch task processing
* Process tasks via shell commands
* CRUD on Tasks store or individual Task

## Planned Features

* REST Service for enqueuing
* Explore options for other languages, for example delegating the execution of a task over nats core request-reply
* A scheduler service that creates tasks on a schedule with scheduled stored in JetStream
* Multiple queues with different priorities accessible in the same client
* Task DLQ for failed or expired task definitions, with various retention policies
* Middleware support for task handlers like http muxes
* Real time lifecycle events
