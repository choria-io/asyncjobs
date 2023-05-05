+++
title = "Tasks lifecycle"
toc = true
weight = 10
+++

A task is the basic item of work that is scheduled and processed by a Handler.

## Task Storage

Tasks are stored in NATS JetStream in a Stream called `CHORIA_AJ_TASKS`. Every Task is stored in a subject keyed by it's ID `CHORIA_AJ.T.<TASK ID>`. Tasks must have unique IDs.

Task storage defaults to being File based and non replicated with no limits on the number of Tasks or how long they are retained. It's recommended that an appropriate retention time is set as detailed below.

On a new setup the Task storage can be initialized with different defaults:

```
$ ajc tasks initialize --retention 1h --memory --replicas 1
Tasks Storage:

         Entries: 0 @ 0 B
    Memory Based: true
        Replicas: 1
  Archive Period: 1h0m0s
```

In code this can be done first time a client starts:

```go
client, err := asyncjobs.NewClient(
        asyncjobs.NatsContext("AJC"), 
        asyncjobs.MemoryStorage(), 
        asyncjobs.StoreReplicas(1), 
        asyncjobs.TaskRetention(time.Hour))
```

Once created the Retention period can be adjusted:

```
$ ajc tasks configure 5h
Tasks Storage:

         Entries: 0 @ 0 B
    Memory Based: true
        Replicas: 1
  Archive Period: 5h0m0s
```

## Task Properties

Tasks have a number of properties that can influence their processing:

| Property           | Description                                                                                                                 |
|--------------------|-----------------------------------------------------------------------------------------------------------------------------|
| `Type`             | A string like `email:new`, the task router would dispatch the Taek to any Handler like `email:new`, `email` or ``           |
| `Payload`          | The content of the task which the handler can read to influence what it does                                                |
| `Deadline`         | Before calling the Handler the Task Deadline will be checked, tasks past their Deadlne will not be processed                |
| `MaxTries`         | Tasks that have already had this many tries will be terminated, defaults to 10 since `0.0.8`                                |
| `Dependencies`     | Task IDs that should all complete successfully before this task will run, since `0.0.8`                                     |
| `LoadDependencies` | For tasks with Dependencies, load dependency `TaskResults` into `DependencyResults` before calling a handler, since `0.0.8` |

Setting other properties on new Tasks should be avoided.

## Task Outcomes

During the lifecycle of a task various properties will be set, `State` is the main one that can be updated many times as per the below section, others will also be set though:

| Property      | Description                                                                              |
|---------------|------------------------------------------------------------------------------------------|
| `Queue`       | Once enqueued this will be the Queue name it is stored in                                |
| `Result`      | On success this structure will be set with the time of completion and the result payload |
| `State`       | As in the section below                                                                  |
| `CreatedAt`   | A time-stamp indicating when the task was firt enqueued                                  |
| `LastTriedAt` | When not nil, this is the last time-stamp a handler was called                           |
| `Tries`       | Is how many times the task have been sent to Handlers                                    |
| `LastErr`     | When not empty this is the text of the most recent error from the Handler                |

## Task States

Tasks have many possible states and the processor will update the task as it traverses the various states.

| State                  | Description                                                                                              |
|------------------------|----------------------------------------------------------------------------------------------------------|
| `TaskStateUnknown`     | No state is set in the task, uninitialised or corrupt task                                               |
| `TaskStateNew`         | A brand new task, either never processed or possibly not enqueued                                        |
| `TaskStateActive`      | A task that is being handled by a handler                                                                |
| `TaskStateRetry`       | A task that had a previous failure and is now scheduled for later retry or one that was manually retried |
| `TaskStateExpired`     | A task that was attempted to be processed but at that time it exceeded its deadline                      |
| `TaskStateTerminated`  | A handler returned an `ErrTerminateTask` error and so will not be retried again                          |
| `TaskStateCompleted`   | Successful completed task                                                                                |
| `TaskStateQueueError`  | Task was created but the Work Queue entry could not be made                                              |
| `TaskStateBlocked`     | When a Task is waiting on it's dependencies (since `0.0.8`)                                              |
| `TaskStateUnreachable` | When a Task cannot execute because a dependent task failed (since `0.0.8`)                               |

Some termination states like when a Queue is configured to only keep Tasks for 5 Hours but a task has had no processor for that entire period will not be reflected in the task state - the task will simply be orphaned.

## Task Dependencies

Since `0.0.8` we support a notion of task dependencies. A task with dependencies will start in `TaskStateBlocked`, when they is scheduled the processor will check all dependencies, if all are complete the task will become Active.

Should one of the dependent tasks have a final failure state this task will become `TaskStateUnreachable` as a final state.

## Retrying a Task

While a Task is still in the Task Store and if it's ID is known it can be retried. Any Work Queue items for the disk will be discarded, the task will be set to `TaskStateRetry`, it's `Result` will be discarded and it will be enqueued again for processing.

```go
err = client.RetryTaskByID(ctx, "24atXzUomFeTt4OK4yNJNafNQR3")
```

The CLI can also retry tasks using `ajc task retry 24atXzUomFeTt4OK4yNJNafNQR3`.

## End State Discard

With no additional actions Tasks are kept either forever or, as above, based on Task Store retention policy.

The client can be configured to discard certain Tasks though:

```go
client, _ := ayncjobs.NewClient(
        ayncjobs.NatsConn(nc), 
        DiscardTaskStates(TaskStateExpired, TaskStateCompleted))
```

Here we configure the client that whenever it sets a task to either `TaskStateExpired` or `TaskStateCompleted` it should save the state and then discard the Task.

The Task is first saved to allow any processes watching task life cycles to get notified. This behavior will change once [#15](https://github.com/choria-io/asyncjobs/issues/15) is completed.

## Flow Diagram

This includes the Task Relationships introduced in `0.0.8`

[![](https://mermaid.ink/img/pako:eNp9U8tu2zAQ_BWCJweIfsCHArblNE5cJ61d5BDlsBHXESGKVJdk20Dyv5ei5GeR8CTszM7sLMWG50YgH_M3grpgmzTTLJzJaAO2XDtwuMI_VyxJvrBpM9e_PHrc9ZxprLbfuxJ7IumQzYkMtex2tIXxFpICtEhqqJHYQS7SI-_qVGYxejJUsogOwCICd80jIXskk6O1u1NkcF7KSjrbsvv_TVNpcyCBYlC86_tSBKGkRvYDIS9QtOzrJwPP_9aSLiSWx_VMcid_4xn67RO1n5o6U3hV5z2ro-JUmbw8GK4i_NCkWO_jP_QxJkqxmalqhS5kWJ5hg0ZI6OidrbuUXmG76EnLSJo1t2FAhTTIzrpqq6XaX2P63Odwha9ebeLrY4yDL3s5bQ6NG6RK6kDpuC2bP3-8iwNVXIp07jf7LRpPbwqsTawDcsf2mG3Y0s0QG_LSbLeXsVnMza95FRxBivC7N10l467ACjM-Dp8CqMx4pneB52vR3byQzhAPQyiL1xy8M-t3nfOxI497UiohPJ1qYO3-AQWxFDQ)](https://mermaid.live/edit#pako:eNp9U8tu2zAQ_BWCJweIfsCHArblNE5cJ61d5BDlsBHXESGKVJdk20Dyv5ei5GeR8CTszM7sLMWG50YgH_M3grpgmzTTLJzJaAO2XDtwuMI_VyxJvrBpM9e_PHrc9ZxprLbfuxJ7IumQzYkMtex2tIXxFpICtEhqqJHYQS7SI-_qVGYxejJUsogOwCICd80jIXskk6O1u1NkcF7KSjrbsvv_TVNpcyCBYlC86_tSBKGkRvYDIS9QtOzrJwPP_9aSLiSWx_VMcid_4xn67RO1n5o6U3hV5z2ro-JUmbw8GK4i_NCkWO_jP_QxJkqxmalqhS5kWJ5hg0ZI6OidrbuUXmG76EnLSJo1t2FAhTTIzrpqq6XaX2P63Odwha9ebeLrY4yDL3s5bQ6NG6RK6kDpuC2bP3-8iwNVXIp07jf7LRpPbwqsTawDcsf2mG3Y0s0QG_LSbLeXsVnMza95FRxBivC7N10l467ACjM-Dp8CqMx4pneB52vR3byQzhAPQyiL1xy8M-t3nfOxI497UiohPJ1qYO3-AQWxFDQ)
