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

Task delivery is handled by `asyncjobs.Mux`.

```go
router := asyncjobs.NewTaskRouter()
router.HandleFunc("email:new", emailNewHandler)
router.HandleFunc("", emailPassthroughHandler)

client.Run(ctx, router)
```

The router above dispatches `email:new` tasks to `emailNewHandler` and all other tasks to `emailPassthroughHandler`. A handler registered for `email:` would process all unmatched email-related tasks.

## Middleware

Cross-cutting behavior such as logging, metrics, tracing, authentication, or panic recovery is typically expressed as middleware. A `Middleware` is a function that wraps a `HandlerFunc` and returns a new one:

```go
func Logging(next asyncjobs.HandlerFunc) asyncjobs.HandlerFunc {
	return func(ctx context.Context, log asyncjobs.Logger, t *asyncjobs.Task) (any, error) {
		start := time.Now()
		res, err := next(ctx, log, t)
		log.Infof("task %s %s took %s err=%v", t.Type, t.ID, time.Since(start), err)
		return res, err
	}
}
```

Middleware should normally invoke `next(ctx, log, t)` and return its result and error unchanged. To short-circuit (for example on an authentication failure) return without calling `next`. Always use the `(ctx, log, t)` arguments passed to the returned closure; capturing values from the surrounding scope at construction time will leak them across dispatches.

### Global Middleware

Use `Mux.Use` to register middleware that applies to every handler:

```go
router := asyncjobs.NewTaskRouter()
router.Use(Recovery, Logging)
router.HandleFunc("email:new", emailNewHandler)
```

Middleware registered earlier runs outermost, so the example above resolves at dispatch time to `Recovery(Logging(emailNewHandler))`. Put a recovery middleware first if you want it to catch panics from later middleware as well as the handler.

`Use` may be called before or after `HandleFunc`; existing handlers are rewrapped so subsequent dispatches see the new chain. Dispatches already in flight keep the chain they previously resolved.

### Per-Route Middleware

`HandleFunc` accepts optional middleware that applies only to that route, inside any global middleware:

```go
router.Use(Logging)
router.HandleFunc("email:new", emailNewHandler, RequireSignedTask)
```

The dispatch chain is `Logging(RequireSignedTask(emailNewHandler))` — global middleware always wraps per-route middleware, which always wraps the handler.

### Reusable Bundles

`asyncjobs.Chain` composes several middlewares into one, useful when the same combination is reused across routes:

```go
secured := asyncjobs.Chain(RequireSignedTask, AuditLog)
router.HandleFunc("email:new", emailNewHandler, secured)
router.HandleFunc("billing:charge", chargeHandler, secured)
```

Order is preserved: `Chain(a, b, c)` runs `a` outermost, then `b`, then `c`.

### Unrouted Tasks

The built-in handler returned for task types with no matching route is intentionally not wrapped by middleware. This keeps unrouted tasks from generating logging or metric noise; if you want to observe them, register a catch-all handler with `HandleFunc("", ...)` or a prefix that matches them.

Both `Use` and `HandleFunc` return `ErrInvalidMiddleware` if any supplied middleware is `nil`.

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
