# Scheduled Tasks

The Task Scheduler supports cron-like entries that create tasks on demand.

A separate supervisor process evaluates the configured schedules and creates the tasks. The supervisor is built into the `ajc` binary and is deployable in any container manager.

The scheduler supports highly-available clusters. Members perform leader election, and one member schedules tasks at a time. Restarts and signals are not required when schedules are added, removed, or updated.

[Deploying to Kubernetes](../handlers-k8s/) is supported through Helm charts. The scheduler can also run anywhere else, as described below.

## Schedule overview

A scheduled task combines a cron-like schedule with task properties. The scheduler creates new jobs on demand, using those properties as templates.

```go
// ScheduledTask represents a cron like schedule and task properties that will
// result in regular new tasks to be created machine schedule
type ScheduledTask struct {
	// Name is a unique name for the scheduled task
	Name string `json:"name"`
	// Schedule is a cron specification for the schedule
	Schedule string `json:"schedule"`
	// Queue is the name of a queue to enqueue the task into
	Queue string `json:"queue"`
	// TaskType is the type of task to create
	TaskType string `json:"task_type"`
	// Payload is the task payload for the enqueued tasks
	Payload []byte `json:"payload"`
	// Deadline is the time after scheduling that the deadline would be
	Deadline time.Duration `json:"deadline,omitempty"`
	// MaxTries is how many times the created task could be tried
	MaxTries int `json:"max_tries"`
	// CreatedAt is when the schedule was created
	CreatedAt time.Time `json:"created_at"`
}
```

Valid schedules use the common unix cron format, including syntax like `*/5`. Shortcuts such as `@yearly`, `@monthly`, `@weekly`, `@daily`, and `@hourly` are supported, along with `@every 10m`, where `10m` is a Go standard duration.

## Go API

Adding and loading scheduled tasks resembles working with a normal task:

```go
client, _ := asyncjobs.NewClient(asyncjobs.NatsContext("AJC"))

// The deadline being an hour from now results in a Scheduled Task with a 1 hour deadline set
task, _ := asyncjobs.NewTask("email:monthly", nil, asyncjobs.TaskDeadline(time.Now().Add(time.Hour)))

// Create the schedule
err := client.NewScheduledTask("EMAIL_MONTHLY_UPDATE", "@monthly", "EMAIL", task)

// Load it
st, _ := client.LoadScheduledTaskByName("EMAIL_MONTHLY_UPDATE")

// Remove it
err = client.RemoveScheduledTask("EMAIL_MONTHLY_UPDATE")
```

## CLI management

The CLI is new, and some aspects may change.

### Adding and removing scheduled tasks

```
$ ajc task cron add EMAIL_MONTHLY_UPDATE "0 0 1 * *" email:monthly --queue EMAIL --deadline 12h
Scheduled Task EMAIL_MONTHLY_UPDATE created at 17 Feb 22 17:40:37 UTC

             Schedule: 0 0 1 * *
                Queue: EMAIL
            Task Type: email:monthly
              Payload: 0 B
  Scheduling Deadline: 12h0m0s
```

The command adds a scheduled task that runs monthly. The shortcut `monthly` also works. Each run creates a task of type `email:monthly` in the `EMAIL` queue with a 12-hour deadline from creation.

To enter the `@yearly` schedule, use just `yearly`. The CLI interprets the `@`.

Remove a scheduled task by name with `ajc task cron delete EMAIL_MONTHLY_UPDATE`. The `-f` flag makes the command non-interactive.

### Viewing schedules

List all schedules:

```
$ ajc task cron list
╭───────────────────────────────────────────────────────────────────────────────────╮
│                                1 Scheduled Task(s)                                │
├──────────────────────┬───────────┬───────┬───────────────┬────────────────────────┤
│ Name                 │ Schedule  │ Queue │ Task Type     │ Created                │
├──────────────────────┼───────────┼───────┼───────────────┼────────────────────────┤
│ EMAIL_MONTHLY_UPDATE │ 0 0 1 * * │ EMAIL │ email:monthly │ 17 Feb 22 17:40:37 UTC │
╰──────────────────────┴───────────┴───────┴───────────────┴────────────────────────╯
$ ajc task cron list --names
EMAIL_MONTHLY_UPDATE
```

View a single schedule:

```
$ ajc task cron view EMAIL_MONTHLY_UPDATE
Scheduled Task EMAIL_MONTHLY_UPDATE created at 17 Feb 22 17:40:37 UTC

             Schedule: 0 0 1 * *
                Queue: EMAIL
            Task Type: email:monthly
              Payload: 0 B
  Scheduling Deadline: 12h0m0s
$ ajc task cron view EMAIL_MONTHLY_UPDATE --json
{
  "name": "EMAIL_MONTHLY_UPDATE",
  "schedule": "0 0 1 * *",
  "queue": "EMAIL",
  "task_type": "email:monthly",
  "payload": null,
  "Deadline": 43200000000000,
  "MaxTries": 0,
  "created_at": "2022-02-17T17:40:37.001480991Z"
}
```

## Running the scheduler

[Deploying to Kubernetes](../handlers-k8s/) is supported through Helm charts. The scheduler can also run elsewhere using the CLI below.

Each scheduler needs a name, ideally unique. The name appears in logs and in leader elections.

```
$ ajc task cron scheduler $(hostname -f) --monitor 8080 --context AJC
INFO[19:21:29] Starting leader election as dev1.example.net
INFO[19:21:29] Registered a new item EMAIL_MONTHLY_UPDATE on queue EMAIL: 0 0 1 * *
INFO[19:21:29] Loaded 1 scheduled task(s)
INFO[19:21:42] Became leader, tasks will be scheduled
```

Logs record schedule additions, schedule removals, and task-creation events. Prometheus metrics are served at port 8080 on `/metrics`.

Any number of schedulers can run. Members perform leader election. All members log schedule updates, but only the leader creates tasks.

> [!info] Note
> During a leadership change, some tasks may not be scheduled. A later release will address this.

The `ajc info` output includes scheduler state:

```
$ ajc info
...
Leader Elections:

         Entries: 1 @ 139 B
    Memory Based: false
        Replicas: 1
       Elections:
                  task_scheduler: dev1.example.net
...
```

The current leader of the `task_scheduler` election group appears in the output.
