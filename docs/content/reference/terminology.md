+++
title = "Terminology"
toc = true
weight = 50
+++

Several terms are used in this system as outlined here.

## JetStream

The underlying storage and work queue manager. See the [NATS project documentation](https://docs.nats.io/nats-concepts/jetstream) for background.

## Work Queue

A Work Queue is JetStream Stream set with `WorkQueue` Retention policy. The underlying Stream holding these queues are called `CHORIA_AJ_Q_DEFAULT` for the `DEFAULT` queue.

## Work Item

Work Items are placed in the Work Queue and scheduled by JetStream. The contents of the Work Queue are `ProcessItem` messages encoded in JSON format.

## Client

Connects to JetStream and manages the enqueueing and routing of tasks.

## Handler

Handlers are functions that can process a task with the signature `func(context.Context, *asyncjobs.Task) (any, error)`.

## Router

The Router locates handlers for a particular task using the `Type` field as a matcher.

See [Routing, Handlers, Concurrency and Retry](../routing-concurrency-retry/).

## Task

A task is a specific kind of Work Item that is handled by a Handler via a Router, this is the main processible unit. In time we anticipate other kinds of Item for example Scheduled items, now the only kind of Item is a Task.

Task have time stamps, statuses and more. See [Task Lifecycle](../task-lifecycle/).

## Lifecycle Event

Events are small messages published to notify listeners about the state of changes. Today only Task State changes are reported, in future we will report more such as Processor start and stop etc.

See [Lifecycle Events](../lifecycle-events/)

## Scheduled Task

A cron like schedule for creating tasks on demand. Requires the running of a Task Scheduler process.  See [Scheduled Tasks](../../overview/scheduled-tasks/)
