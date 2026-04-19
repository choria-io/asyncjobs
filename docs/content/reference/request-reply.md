+++
title = "Request-Reply Handlers"
description = "Implement handlers in other languages via NATS Request-Reply"
toc = true
weight = 30
+++

Handlers are typically implemented in Go and compiled into the binary for best performance.

Other programming languages are supported through [NATS Request-Reply](https://docs.nats.io/nats-concepts/core-nats/reqreply). A remote service can be written in any language and called out to over NATS.

Review [Routing, Handlers, Concurrency, and Retry](../routing-concurrency-retry/) first for background. Remote handlers map directly onto the same model.

## Registering with the router

```go
client, _ := asyncjobs.NewClient(asyncjobs.NatsConn(nc), asyncjobs.BindWorkQueue("EMAIL"))

router := asyncjobs.NewTaskRouter()
router.RequestReply("email:new", client)

client.Run(router)
```

This registers with the router for tasks of type `email:new`. Tasks of that type are dispatched via NATS Request-Reply.

When all handlers in a deployment use request-reply, the [Docker-based runner](../overview/handlers-docker/) achieves the same outcome without any Go code.

## Protocol

A lightweight JSON-plus-headers protocol communicates with the remote service. The protocol supports returning errors, including the `ErrTerminateTask` behavior.

The service must listen on `CHORIA_AJ.H.T.email:new`, typically in a NATS queue group, replacing `email:new` with the configured task type. A handler registered with task type `""` handles all tasks, and the service listens on `CHORIA.AJ.H.T.catchall`.

### Tasks

A request for a task handler carries these headers:

| Header                | Value                               |
|-----------------------|-------------------------------------|
| `AJ-Content-Type`     | `application/x-asyncjobs-task+json` |
| `AJ-Handler-Deadline` | `2009-11-10T23:00:00Z`              |

The content type is identical for all tasks. The deadline is a UTC timestamp by which the remote service must complete handling to avoid a timeout.

The body is a JSON-encoded `Task`.

Responses from the service may set these headers:

| Header         | Description                                                                                                                    |
|----------------|--------------------------------------------------------------------------------------------------------------------------------|
| `AJ-Error`     | Indicates an error was encountered; the value is set as the task error and the task is retried later                           |
| `AJ-Terminate` | Terminates the task via `ErrTerminateTask`; the value becomes additional text in the error. No further retries are performed   |

The body of the response is stored on the task unmodified.

## Demonstration

The `nats` CLI tool shows this in action:

```
$ nats reply CHORIA_AJ.H.T.email:new 'success' --context AJC
18:33:32 [#1] Received on subject "CHORIA_AJ.H.T.email:new":
18:33:33 AJ-Content-Type: application/x-asyncjobs-task+json
18:33:33 AJ-Handler-Deadline: 2022-02-09T17:34:31Z

{"id":"24smZHaWnjuP371iglxeQWK7nOi","type":"email:new","queue":"DEFAULT","payload":"InsuLi4ufSI=","state":"active","created":"2022-02-09T17:28:41.943198067Z","tried":"2022-02-09T17:33:33.005041134Z","tries":5}
```

The CLI received the job with the two headers set and the expected payload, responded with `success`, and the task completed.

```
$ ajc task view 24smZHaWnjuP371iglxeQWK7nOi --json
{
  "id": "24smZHaWnjuP371iglxeQWK7nOi",
  "type": "email:new",
  "queue": "DEFAULT",
  "payload": "InsuLi4ufSI=",
  "result": {
    "payload": "dGVzdA==",
    "completed": "2022-02-09T17:33:33.00755251Z"
  },
  "state": "complete",
  "created": "2022-02-09T17:28:41.943198067Z",
  "tried": "2022-02-09T17:33:33.007552104Z",
  "tries": 5
}
```
