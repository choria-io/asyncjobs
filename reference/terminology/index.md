# Terminology

Several terms recur throughout this system.

## JetStream

The underlying storage and work queue manager. See the [NATS project documentation](https://docs.nats.io/nats-concepts/jetstream) for background.

## Work queue

A work queue is a JetStream stream with the `WorkQueue` retention policy. The underlying stream backing the `DEFAULT` queue is called `CHORIA_AJ_Q_DEFAULT`.

## Work item

Work items are placed in the work queue and scheduled by JetStream. The contents of the work queue are `ProcessItem` messages encoded as JSON.

## Client

Connects to JetStream and manages the enqueueing and routing of tasks.

## Handler

A handler is a function with the signature `func(context.Context, *asyncjobs.Task) (any, error)`.

## Router

The router locates handlers for a task using the `Type` field as a matcher.

See [Routing, Handlers, Concurrency, and Retry](../routing-concurrency-retry/).

## Task

A task is a specific kind of work item handled by a handler via the router. Tasks are the primary unit of work. Other kinds of work item are anticipated, such as scheduled items. At present, tasks are the only kind.

Tasks have timestamps, statuses, and more. See [Task Lifecycle](../task-lifecycle/).

## Lifecycle event

A lifecycle event is a small message published to notify listeners about state changes. Only task state changes are reported today. Processor start and stop events and similar signals will follow.

See [Lifecycle Events](../lifecycle-events/).

## Scheduled task

A cron-like schedule that creates tasks on demand. A task scheduler process must be running. See [Scheduled Tasks](../../overview/scheduled-tasks/).
