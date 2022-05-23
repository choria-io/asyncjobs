+++
title = "Choria Async Jobs"
weight = 5
+++

## Overview

`asyncjobs` provide a [JetStream](https://docs.nats.io/jetstream) backed Asynchronous Job Processing system in Go.

Use it to schedule and run jobs in a distributed cluster of workers.  Workers can be written in any language and scales
horizontally and vertically.

You can think of it like a distributed share-nothing cron.

## Features

This feature list is incomplete, at present the focus is on determining what will work well for the particular patterns
JetStream enables, so there might be some churn in the feature set here.

### Tasks

* Task definitions stored post-processing, with various retention and discard policies
* Ability to retry a Task that has already been completed or failed
* Task deduplication
* Deadline per task - after this time the task will not be processed
* Tasks can depend on other tasks
* Max tries per task, capped to the queue tries
* Task state tracked throughout it's lifecycle
* [K-Sortable](https://github.com/segmentio/ksuid) Task GUIDs
* Lifecycle events published about [changes to task states](Lifecycle-Events)

See [Task Lifecycle](https://github.com/choria-io/asyncjobs/wiki/Task-Lifecycle) for full background and details

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
* Support for Handlers in all NATS Supported languages using [Remote Handlers](Remote-Request-Reply-Handlers)
* Statistics via Prometheus

### Storage

* Replicated storage using RAFT protocol within JetStream Streams, disk based or memory based
* KV for configuration and schedule storage
* KV for leader elections

### Scheduled Tasks

* Cron like schedules creating tasks on demand
* HA capable Scheduler process integrated with `ajc`
* Prometheus monitoring
* CLI CRUD operations via `ajc task cron`

See [Scheduled Tasks](Scheduled-Tasks)

### Misc

* Supports NATS Contexts for connection configuration
* Supports custom loggers, defaulting to go internal `log`

### Command Line

* Various info and state requests
* Configure aspects of Task and Queue storage
* Watch task processing
* Process tasks via shell commands
* CRUD on Tasks store or individual Task
* CRUD on Scheduled Tasks

## Planned Features

A number of features are planned in the near term, see our [GitHub Issues](https://github.com/choria-io/asyncjobs/labels/enhancement)
