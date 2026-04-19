# Lifecycle Events

Lifecycle events are small JSON messages published to notify observers about stages of task processing and client life.

Only one event type is supported today: a notification about changes to task state. Additional events will follow. A future release will emit Cloud Events standard messages.

Events are informational. Delivery is not guaranteed and events are not persisted. They can loosely couple systems that react to task completion, but they must not be relied on for exactly-once or guaranteed notification.

## Event types

Each event carries a type such as `io.choria.asyncjobs.v1.task_state`, exposed in Go as `asyncjobs.TaskStateChangeEventType`. The type string aids parsing and routing through other systems.

## Parsing an event

A helper parses any supported event for use with the common Go type-switch pattern:

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

This event is published for any state change of a task. A task can be watched by ID, or all tasks can be watched at once.

These events are published to `CHORIA_AJ.E.task_state.*`, where the final token is the job ID.

On the wire, the messages look like this. The `task_age` field is a Go duration.

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
