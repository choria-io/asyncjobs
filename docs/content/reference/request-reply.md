+++
title = "Request-Reply Handlers"
toc = true
weight = 30
+++

Typically, and for best performance, you implement your handlers in Go and compile them into the binary.

In order to support other programming languages we also support calling out over NATS in a [Request-Reply](https://docs.nats.io/nats-concepts/core-nats/reqreply) fashion to a service that can be programmed in any language.

It's worth understanding [Routing, Handlers, Concurrency and Retry](../routing-concurrency-retry/) for background, these remote callout Handlers map exactly onto that model.

## Registering with the Router

```go
client, _ := asyncjobs.NewClient(asyncjobs.NatsConn(nc), asyncjobs.BindWorkQueue("EMAIL"))

router := asyncjobs.NewTaskRouter()
router.RequestReply("email:new", client)

client.Run(router)
```

Here we register with the Router for tasks of type `email:new` that will call out via Request-Reply.

If all your Handlers are of this type I strongly suggest investigating our [Docker Based Runner](../overview/handlers-docker/) that can achieve this without writing any Go code.

## Protocol

We implement a light-weight JSON + Headers protocol to communicate with remote services. They support returning errors including the `ErrTerminateTask` behavior.

Your service must listen on `CHORIA_AJ.H.T.email:new` - most probably in a queue group - where you would replace `email:new` with whatever you chose as a task type. A handler that is registered with task type `""` will handle all tasks of all types and the handling service should listen on `CHORIA.AJ.H.T.catchall`.

### Tasks

A request for a Task Handler will have these headers:

| Header                | Value                               |
|-----------------------|-------------------------------------|
| `AJ-Content-Type`     | `application/x-asyncjobs-task+json` |
| `AJ-Handler-Deadline` | `2009-11-10T23:00:00Z`              |

The content-type is same for all Tasks, the Deadline is a UTC timestamp indicating by what time the remote service has to complete handling the task to avoid timeouts.

The body is simply a JSON format `Task`.

Responses from your service can have these headers:

| Header         | Description                                                                                                                    |
|----------------|--------------------------------------------------------------------------------------------------------------------------------|
| `AJ-Error`     | Indicates an error was encountered, the value is set as task error, the task is retried later                                  |
| `AJ-Terminate` | Terminates the task via `ErrTerminateTask`, the value will be set as additional text in the error. No further retries are done |

The body of your response is taken and stored with the Task unmodified.

## Demonstration

To see this in action, we can use the `nats` CLI tool.

```
$ nats reply CHORIA_AJ.H.T.email:new 'success' --context AJC
18:33:32 [#1] Received on subject "CHORIA_AJ.H.T.email:new":
18:33:33 AJ-Content-Type: application/x-asyncjobs-task+json
18:33:33 AJ-Handler-Deadline: 2022-02-09T17:34:31Z

{"id":"24smZHaWnjuP371iglxeQWK7nOi","type":"email:new","queue":"DEFAULT","payload":"InsuLi4ufSI=","state":"active","created":"2022-02-09T17:28:41.943198067Z","tried":"2022-02-09T17:33:33.005041134Z","tries":5}
```

The CLI received the jobs with the 2 headers set and appropriate payload, it responsed with `success` and the task was completed.

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
