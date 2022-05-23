+++
title = "Lifecycle Events"
toc = true
weight = 40
+++

Lifecycle events are small JSON messages that are published to notify about various stages of processing and the life of a client.

Today the only event we support is one notifying about changes in Task State but more will be added. In future we will support emitting Cloud Event standard events.

Events are not guaranteed to be delivered and are not persisted, they are informational. While you can use them to build a kind of coupled system of waiting for a task to complete you should not rely on these events to be delivered in 100% of cases.

## Event Types

Each event has a type like `io.choria.asyncjobs.v1.task_state` aka `asyncjobs.TaskStateChangeEventType` that can help with parsing and routing of events through other systems.

## Parsing an event

We provide a helper to parse any supported event and to process them using the common go type switch pattern.

```go
// subscribe to all events
sub, err := nc.SubscribeSync(asyncjobs.EventsSubjectWildcard)
panicIfErr(err)

for {
	msg, _ := sub.NextMsg(time.Minute)
	event, kind, _ := asyncjobs.ParseEventJSON(msg.Data)

	switch e := event.(type) {
	case asyncjobs.TaskStateChangeEvent:
		// handle task state change event

	default:
		// logs the io.choria.asyncjobs.v1.task_state style task type
		log.Printf("Unknown event type %s", kind)
	}
}
```

## `TaskStateChangeEvent`

This event type is published for any state change of a Task, using it you can watch a task by ID or all tasks.

These events are published to `CHORIA_AJ.E.task_state.*` with the last token being the Job ID.

On the wire the messages look like here, with `task_age` being a go Duration.

```json
{
  "event_id": "24mHmiRY9eQCVU4xuHwsztJ2MJH",
  "type": "io.choria.asyncjobs.v1.task_state",
  "timestamp": "2022-02-07T10:16:42Z",
  "task_id": "24mHmkobHqLE6bxiWPTwuV30xrO",
  "state": "complete",
  "tries": 1,
  "queue": "DEFAULT",
  "task_type": "email:new",
  "task_age": 4037478
}
```
