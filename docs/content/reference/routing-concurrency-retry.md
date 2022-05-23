+++
title = "Routing, Concurrency, Retry"
toc = true
weight = 20
+++

Processing Tasks is what it's all about, so, this is an important topic to explore and understand. It is quite simple in the general case but there are some nuances to be aware of.

Handlers are how Tasks get executed, typically this is code you provide written in Go.

For a non Go solution see [Remote Request Reply Handlers](../request-reply/), but reading this page is still good for a grounding understanding since remote Handlers map exactly onto the same concepts.

## Example

Below is a handler that sends an email, the task Payload is a serialized object describing an email to send.  The deadlines and timeouts are extracted from the Context and the mail is sent.

The Task handler then is a single-purpose piece of code capable of handling 1 type of Task.

```go
func emailNewHandler(ctx context.Context, log asycjobs.Logger, task *asyncjobs.Task) (interface{}, error) {
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

## Routing Tasks to Handlers

Every client that processes messages must be ready to process all messages found in the Queue. So if you have an `EMAIL` queue, all running clients must be able to handle all Tasks.

Should there be no appropriate handler the message will fail and enter retries.

Task delivery is handled by `asyncjobs.Mux` which today is quite minimal, we plan to support Middleware and more later.

```go
router := asyncjobs.NewTaskRouter()
router.HandleFunc("email:new", emailNewHandler)
router.HandleFunc("", emailPassthroughHandler)

client.Run(ctx, router)
```

Here we set up the above example handler to handle `email:new` messages and register an handler for other messages.  A handler could be set to handle `email:` messages and it would process all unhandled email related messages.

## Concurrency

There are 2 kinds of Concurrency control in effect at any time: Client and Queue.

### Client Concurrency

Every client can limit how many concurrent tasks it wish to handle. You might have 4 cores in your instance and so want to run only 4 Handlers at a time.

```go
client, err := asyncjobs.NewClient(asyncjobs.ClientConcurrency(runtime.NumCPU()))
```

Here we set the client to use `runtime.NumCPU()` to dynamically allocate maximum concurrency based on available logical CPUs.

### Queue Concurrency

When many clients are active against a specific Queue they would all get jobs according to the limit above. You might also want to limit the overall concurrency of all email processing regardless of how many clients you have.  With 10 clients each set to allow 10 concurrent you would be handling 100 tasks, but if you know your infrastructure can only support 50 at a time you can limit this on the Queue.

```go
queue := &asyncjobs.Queue{
	Name: "EMAIL",
	MaxConcurrent: 50
}
client, err := asyncjobs.NewClient(asyncjobs.WorkQueue(queue))
```

This would create a new Queue the first time and set it to 50 maximum concurrent handlers - regardless of how many your clients start.

You can adjust this once created using `ajc queue configure EMAIL --concurrent 100`.

## Task Runtime and Max Tries

The Queue defines how long a Task can be processed, a Task that is not done being processed by that timeout will result in a retry - on the assumption that the handler has crashed. You should set the timeout carefully to avoid duplicate task handling.

```go
queue := &asyncjobs.Queue{
	Name: "EMAIL",
	MaxRunTime: time.Hour,
	MaxTries: 100,
}
```

Above we define a Queue that will allow a task to be handled for up to 1 hour and will retry it 100 times. Care should be taken to pick these values correctly.

The `ajc` command line utility can adjust these times post-creation but running clients will still create context Deadlines based on the configuration that was set when they were started.

## Terminating Processing

In the earlier example we had these 2 lines:

```go
email, err := parseEmail(task.Payload)
if err != nil { return nil, err }
```

This would return the parse error from your Handler, the task would then go and get retried later. Thing is if this is a bad Payload it will never pass processing, invalid JSON will always be invalid JSON.  You might want to give up on the task early:

```go
email, err := parseEmail(task.Payload)
if err != nil { 
	return nil, fmt.Errorf("invalid task payload: %w", asyncjobs.ErrTerminateTask)
}
```

Here we return an error that is a `asyncjobs.ErrTerminateTask`, the task would then be terminated immediately, no future tries will be done and the task state will be set to `TaskStateTerminated`.

## Retry Schedules

When a client determines that a Task has failed and needs to be retried it does so based on a `RetryPolicy`. The default policy is to retry at increasing intervals between 1 minute and 10 minutes with a jitter applied.

To change to a 50 step policy ranging between 10 minutes and 1 hour use:

```go
client, err := asyncjobs.NewClient(RetryBackoffPolicy(asyncjobs.RetryLinearOneHour))
```

We have `RetryLinearTenMinutes`, `RetryLinearOneHour` and `RetryLinearOneMinute` pre-defined.

You can create your own schedule - perhaps based on an exponential backoff - by filling in your values in `asyncjobs.RetryPolicy` or by implementing the `asyncjobs.RetryPolicyProvider` interface.
