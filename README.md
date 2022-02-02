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
 * Examples
   * [Golang](https://github.com/choria-io/asyncjobs/wiki/Introductory-Golang-Walkthrough)
   * [CLI](https://github.com/choria-io/asyncjobs/wiki/Introductory-CLI-Walkthrough)
 
 [![Go Reference](https://pkg.go.dev/badge/github.com/choria-io/asyncjobs.svg)](https://pkg.go.dev/github.com/choria-io/asyncjobs)
 [![Go Report Card](https://goreportcard.com/badge/github.com/choria-io/asyncjobs)](https://goreportcard.com/report/github.com/choria-io/asyncjobs) 
 [![CodeQL](https://github.com/choria-io/asyncjobs/workflows/CodeQL/badge.svg)](https://github.com/choria-io/asyncjobs/actions/workflows/codeql.yaml) 
 [![Unit Tests](https://github.com/choria-io/asyncjobs/actions/workflows/test.yaml/badge.svg)](https://github.com/choria-io/asyncjobs/actions/workflows/test.yaml)

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

A number of features are planned in the near term, see our [GitHub Issues](https://github.com/choria-io/asyncjobs/labels/enhancement)
