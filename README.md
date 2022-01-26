# JetStream Async Jobs

## Features

### Tasks

 * Task definitions stored post-processing, with various retention policies client opt `TaskRetention()`
 * Task DLQ for failed or expired task definitions, with various retention policies (planned)
 * Task deduplication
 * Deadline per task - after this time the task will not be processed
 * Max tries per task, capped to the queue tries
 * Task state tracked during it's lifetime
 * [K-Sortable](https://github.com/segmentio/ksuid) Task GUIDs

### Queues

 * Weighted Priority Queues
 * Strict priority queues (planned)
 * Queues with caps on queued items and different queue-full behaviors (DiscardOld on queue, sets task to TaskStateQueueError)
 * Default or user supplied queue definitions

### Processing

 * Retries of failed tasks with backoff schedules (todo: configurable schedule)
 * Parallel processing of tasks, horizontally or vertically scaled. Run time adjustable upper boundary on a per-queue basis (planned, need a queue max concurrency and a worker process max concurrency, now its 1 setting)
 * Worker crashes does not impact the work queue
 * Handler interface with (planned) middleware
 * Statistics via Prometheus (planned)
 * Real time lifecycle events (planned)

### Storage

 * Replicated storage using RAFT protocol, disk based or memory based (planned)

### Misc

* Task Scheduling via external Scheduler (planned)
