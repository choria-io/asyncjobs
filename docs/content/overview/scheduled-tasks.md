+++
title = "Scheduled Tasks"
toc = true
weight = 20
+++

The Task Scheduler allows you to create cron like entries that will create Tasks on demand.

This requires a separate process to be run that will supervise the configured schedules and create the tasks.  We have such a Scheduler built into the `ajc` binary deployable in any container manager.

The scheduler we provide support being deployed in a highly-available cluster, they will perform leader election with one of the cluster scheduling tasks. There is no need to restart or signal these schedulers as tasks are added, removed or updated.

You can [Deploy to Kubernetes](../handlers-k8s/) using our Helm Charts, or run anywhere else as per below guide.

## Schedule Overview

A Scheduled task takes a Cron like Schedule and some Task related properties and will then create new jobs on demand using those properties as templates.

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

Valid schedules are like those for the common unix utility cron, including the syntaxes like `*/5` and so forth. Short cuts for `@yearly`, `@monthly`, `@weekly`, `@daily` and `@hourly` are supported in addition to `@every 10m` where `10m` is a Go standard duration.


## Go API

Adding and loading Scheduled Tasks is a lot like a normal task:

```go
client, _ := aj.NewClient(asyncjobs.NatsContext("AJC"))

// The deadline being an hour from now will result in a Schedule Task with a 1 hour deadline set
task, _ := aj.NewTask("email:monthly", nil, aj.TaskDeadlin(time.Now().Add(time.Hour)))

// Create the schedule
err := client.NewScheduledTask("EMAIL_MONTHLY_UPDATE", "@monthly", "EMAIL", task)

// Load it
st, _ := client.LoadScheduledTaskByName("EMAIL_MONTHLY_UPDATE")

// Remove it
err = client.RemoveScheduledTask("EMAIL_MONTHLY_UPDATE")
```

## CLI management

Below a quick overview of the CLI, the CLI is brand new so some aspects might change.

### Adding and Removing Scheduled Tasks

```
$ ajc task cron add EMAIL_MONTHLY_UPDATE "0 0 1 * *" email:monthly --queue EMAIL --deadline 12h
Scheduled Task EMAIL_MONTHLY_UPDATE created at 17 Feb 22 17:40:37 UTC

             Schedule: 0 0 1 * *
                Queue: EMAIL
            Task Type: email:monthly
              Payload: 0 B
  Scheduling Deadline: 12h0m0s
```

We add a scheduled task that will run monthly (Could also use `monthly` instead of cron format), it will create a task with type `email:monthly` in the `EMAIL` queue and we set a deadline on the task 12 hours after creation.

To enter the schedule `@yearly` use just "yearly" as the CLI package will interpret the `@`.

Given the name, you can remove it again with `ajc task cron delete EMAIL_MONTHLY_UPDATE`, it supports `-f` for non interactive.

### Viewing schedules

A list of schedules can be loaded:

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

A single schedule can be viewed:

```
$ ajc task cron view EMAIL_MONTHLY_UPDATE
Scheduled Task EMAIL_MONTHLY_UPDATE created at 17 Feb 22 17:40:37 UTC

             Schedule: 0 0 1 * *
                Queue: EMAIL
            Task Type: email:monthly
              Payload: 0 B
  Scheduling Deadline: 12h0m0s
$ ajc task cron viww EMAIL_MONTHLY_UPDATE --json
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

## Running the Scheduler

You can [Deploy to Kubernetes](../handlers-k8s/) using our Helm Charts, or run anywhere else as per below guide.

Each scheduler needs an, ideally unique, name. This will be used in some logs and in leader elections.

```
$ ajc task cron scheduler $(hostname -f) --monitor 8080 --context AJC
INFO[19:21:29] Starting leader election as dev1.example.net
INFO[19:21:29] Registered a new item EMAIL_MONTHLY_UPDATE on queue EMAIL: 0 0 1 * *
INFO[19:21:29] Loaded 1 scheduled task(s)
INFO[19:21:42] Became leader, tasks will be scheduled
```

As Scheduled jobs are added or removed logs will be logged and updates about scheduling tasks will be shown.  Prometheus metrics is on port 8080 in `/metrics`.

You can run as many schedulers as you want, they will do leader elections, all will log Schedule updates but only one will create Tasks. **NOTE** there's a chance that during a leadership change some tasks will not be scheduled, this is something we will resolve later.

When running `ajc info` will have some details:

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

Here we see the current leader of the `task_scheduler` election group is the instance above.
