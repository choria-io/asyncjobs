# Feature List

This list is incomplete. The focus at present is on determining which patterns work well with JetStream, so the feature set may still change.

## Tasks

* Task definitions stored post-processing, with various retention and discard policies
* Retry a task that has already been completed or failed
* Task deduplication
* Deadline per task, after which the task is not processed
* Tasks can depend on other tasks
* Max tries per task, capped to the queue tries
* Task state tracked throughout its lifecycle
* [K-Sortable](https://github.com/segmentio/ksuid) task GUIDs
* Lifecycle events published about [changes to task states](../../reference/lifecycle-events/)

See [Task Lifecycle](../../reference/task-lifecycle/) for full background and details.

## Queues

* Queues can store different types of task
* Caps on queued items and configurable queue-full behaviors
* Default or user-supplied queue definitions
* Queue per client, many clients per queue

## Processing

* Retries of failed tasks with backoff schedules configurable through `RetryBackoffPolicy()`. Handler opt-in early termination.
* Parallel processing of tasks, horizontally or vertically scaled. Run-time upper boundary adjustable per queue.
* Worker crashes do not impact the work queue.
* Handler interface with task router that selects a handler by task type, with wildcard matches.
* Support for handlers in all NATS-supported languages through [Remote Handlers](../../reference/request-reply/).
* Statistics via Prometheus.

## Storage

* Replicated storage using the RAFT protocol within JetStream Streams, disk-based or memory-based
* KV for configuration and schedule storage
* KV for leader elections

## Scheduled tasks

* Cron-like schedules creating tasks on demand
* HA-capable scheduler process integrated with `ajc`
* Prometheus monitoring
* CLI CRUD operations via `ajc task cron`

See [Scheduled Tasks](../scheduled-tasks/).

## Misc

* Supports NATS Contexts for connection configuration
* Supports custom loggers, defaulting to the Go standard `log` package

## Command line

* Various info and state requests
* Configure aspects of task and queue storage
* Watch task processing
* Process tasks through shell commands
* CRUD on the task store or an individual task
* CRUD on scheduled tasks

## Planned features

Several features are planned in the near term. See the [GitHub Issues](https://github.com/choria-io/asyncjobs/labels/enhancement).
