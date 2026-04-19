+++
title = "Routing, Concurrency, Retry"
description = "Dispatch tasks to handlers with middleware, limits, and retries"
toc = true
weight = 20
+++

Processing tasks is the core of the system. The general case is straightforward, but several nuances are worth knowing.

Handlers execute tasks. Handlers are typically code supplied by the caller, written in Go.

For a non-Go solution, see [Remote Request-Reply Handlers](../request-reply/). This page still provides useful grounding, since remote handlers map directly onto the same concepts.

## Example

The following handler sends an email. The task payload is a serialized object describing the email. Deadlines and timeouts come from the context.

A task handler is a single-purpose piece of code capable of handling one type of task.

```go
func emailNewHandler(ctx context.Context, log asyncjobs.Logger, task *asyncjobs.Task) (any, error) {
	// Parse the task payload into an email
	email, err := parseEmail(task.Payload)
	if err != nil { return nil, err }

	// extract the deadline from the context
	deadline, ok := ctx.Deadline()
	if !ok { deadline = time.Now().Add(time.Minute) }

	// send the email using github.com/xhit/go-simple-mail
	server := mail.NewSMTPClient()
        server.Host = "smtp.example.com"
	server.Port = 587
	server.ConnectTimeout = 2 * time.Second
	server.SendTimeout = time.Until(deadline)
	
	client, err := server.Connect()
	if err != nil { return nil, err }

	client.SetFrom(EmailFrom).AddTo(email.To).SetSubject(email.Subject).SetBody(mail.TextHTML, email.Body)
	err = email.Send(client)
	if err != nil { return nil, err }

	log.Infof("Sent an email to %s", email.To)

        return "success", nil
}
```

## Routing tasks to handlers

Every client that processes messages must be ready to process every message found in the queue. A client connected to an `EMAIL` queue must handle every task on that queue.

A message with no matching handler fails and enters retries.

Task delivery is handled by `asyncjobs.Mux` which today is quite minimal, we plan to support Middleware and more later.

```go
router := asyncjobs.NewTaskRouter()
router.HandleFunc("email:new", emailNewHandler)
router.HandleFunc("", emailPassthroughHandler)

client.Run(ctx, router)
```

The router above dispatches `email:new` tasks to `emailNewHandler` and all other tasks to `emailPassthroughHandler`. A handler registered for `email:` would process all unmatched email-related tasks.

## Concurrency

Two kinds of concurrency control are in effect at any time: client and queue.

### Client concurrency

Every client can limit how many concurrent tasks it handles. A host with four cores might run only four handlers at a time.

```go
client, err := asyncjobs.NewClient(asyncjobs.ClientConcurrency(runtime.NumCPU()))
```

`runtime.NumCPU()` dynamically allocates maximum concurrency based on available logical CPUs.

### Queue concurrency

When many clients are active against a specific queue, each receives jobs up to its own limit. Overall concurrency across a queue can also be capped. With 10 clients, each allowing 10 concurrent tasks, the total would be 100; an infrastructure that only supports 50 at a time can enforce that on the queue.

```go
queue := &asyncjobs.Queue{
	Name: "EMAIL",
	MaxConcurrent: 50
}
client, err := asyncjobs.NewClient(asyncjobs.WorkQueue(queue))
```

This creates a new queue on first use and caps it at 50 concurrent handlers, regardless of how many clients start.

Adjust the value after creation with `ajc queue configure EMAIL --concurrent 100`.

## Task runtime and max tries

The queue defines how long a task can be processed. A task still running past that timeout is retried, on the assumption that the handler has crashed. Choose the timeout carefully to avoid duplicate handling.

```go
queue := &asyncjobs.Queue{
	Name: "EMAIL",
	MaxRunTime: time.Hour,
	MaxTries: 100,
}
```

The queue above allows a task to be handled for up to one hour and retries up to 100 times. Choose these values with care.

The `ajc` CLI can adjust these values post-creation. Running clients still build context deadlines from the configuration present when they were started.

## Terminating processing

The earlier example contained:

```go
email, err := parseEmail(task.Payload)
if err != nil { return nil, err }
```

This returns the parse error from the handler, and the task is retried later. A bad payload will never parse; invalid JSON will always be invalid JSON. In that case, give up on the task immediately:

```go
email, err := parseEmail(task.Payload)
if err != nil { 
	return nil, fmt.Errorf("invalid task payload: %w", asyncjobs.ErrTerminateTask)
}
```

The returned error wraps `asyncjobs.ErrTerminateTask`. The task is terminated immediately, no further retries run, and the state is set to `TaskStateTerminated`.

## Retry schedules

When the client determines that a task has failed and must be retried, it consults a `RetryPolicy`. The default retries at increasing intervals between one and ten minutes, with jitter applied.

To switch to a 50-step policy ranging from 10 minutes to 1 hour:

```go
client, err := asyncjobs.NewClient(RetryBackoffPolicy(asyncjobs.RetryLinearOneHour))
```

The predefined policies are `RetryLinearTenMinutes`, `RetryLinearOneHour`, and `RetryLinearOneMinute`.

Custom schedules, such as exponential backoff, can be built by populating `asyncjobs.RetryPolicy` or by implementing the `asyncjobs.RetryPolicyProvider` interface.
