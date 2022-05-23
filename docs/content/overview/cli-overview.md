+++
title = "CLI Walkthrough"
toc = true
weight = 20
+++

This is a basic walkthrough of publishing Tasks and handling them using the CLI.

This is essentially the CLI version of [Introductory Golang Walkthrough](../golang-overview/).

This guide is known to work with Release 0.0.4

We have a similar [video walkthrough](https://www.youtube.com/watch?v=yRbPCpGsgq4) that discuss the CLI and related topics.

## Requirements

You'll need the [NATS CLI](github.com/nats-io/natscli), an optional JetStream Server and the Async Jobs CLI,

```
$ go install github.com/choria-io/asyncjobs/ajc@v0.0.4
```

### JetStream

If you have an existing JetStream server add a context to connect to it:

```
$ nats context add AJC --server jetstream.example.net:4222
```

If you do not yet have JetStream you can start a local development copy easily, it will create a `AJC` context for you:

```
$ nats server run --jetstream AJC
...
[21398] [INF] Starting JetStream
[21398] [INF]     _ ___ _____ ___ _____ ___ ___   _   __  __
[21398] [INF]  _ | | __|_   _/ __|_   _| _ \ __| /_\ |  \/  |
[21398] [INF] | || | _|  | | \__ \ | | |   / _| / _ \| |\/| |
[21398] [INF]  \__/|___| |_| |___/ |_| |_|_\___/_/ \_\_|  |_|
[21398] [INF]
[21398] [INF]          https://docs.nats.io/jetstream
[21398] [INF]
[21398] [INF] ---------------- JETSTREAM ----------------
[21398] [INF]   Max Memory:      7.20 GB
[21398] [INF]   Max Storage:     6.85 GB
[21398] [INF]   Store Directory: "/home/rip/.local/share/nats/AJC/jetstream"
[21398] [INF] -------------------------------------------
[21398] [INF] Listening for client connections on 0.0.0.0:45913
[21398] [INF] Server is ready
```

## Creating Queues

A Queue is where messages go, you can have many different, named, queues if you wish. If you do not specify any Queue a default one is made called DEFAULT.

You might make different Queues to set different concurrency limits, different max attempts, maximum validity and more.

```
$ ajc queue add EMAIL --run-time 1h --tries 50
EMAIL Work Queue:

         Entries: 0 @ 0 B
    Memory Based: false
        Replicas: 1
  Archive Period: forever
  Max Task Tries: 50
    Max Run Time: 1h0m0s
  Max Concurrent: 100
     Max Entries: unlimited
```

Here we attach to or create a new queue called EMAIL setting some specific options.

## Creating and Enqueueing Tasks

A task can be anything you wish as long as it can serialize to JSON. Tasks have types like email:new, email-new or really anything you want, we'll see later how task types interact with the routing system.

Any number of producers can create tasks from any number of different processes.

```
$ ajc task add --queue EMAIL email:new '{"to":"user@example.net", "subject":"Test Subject", "body":"Test Body"}'
Enqueued task 24YUZF4MzOCLgI7kpwrGtT4lYnS
$ ajc task view 24YUZF4MzOCLgI7kpwrGtT4lYnS
Task 24YUZF4MzOCLgI7kpwrGtT4lYnS created at 02 Feb 22 13:04:26 UTC

              Payload: 85 B
               Status: new
                Queue: EMAIL
                Tries: 0
```

## Consuming and Processing Tasks

The CLI can process tasks through a shell command, lets create a basic command

```
$ touch /tmp/send-email.sh
$ vi /tmp/send-email.sh
$ chmod a+x /tmp/send-email.sh
```

```shell
#!/bin/bash

echo '{"status":"success"}'
```

Now lets run the jobs in our EMAIL queue, 5 concurrently:

```
$ ajc task process "" EMAIL 5 /tmp/send-email.sh --monitor 8080
WARN[0000] Exposing Prometheus metrics on port 8080
INFO[0000] Running task 24YUZF4MzOCLgI7kpwrGtT4lYnS try 1
INFO[0000] Task 24YUZF4MzOCLgI7kpwrGtT4lYnS completed after 2.425439ms and 1 tries with 18 B payload
```

This will process all tasks - `""` task type - via `/tmp/send-email.sh`. You can curl to `http://localhost:8080/metrics` to see Prometheus stats.

Afterward your task will be shown as done:

```
$ ajc task view 24YUZF4MzOCLgI7kpwrGtT4lYnS
Task 24YUZF4MzOCLgI7kpwrGtT4lYnS created at 02 Feb 22 13:04:26 UTC

              Payload: 85 B
               Status: complete
            Completed: 02 Feb 22 13:07:09 UTC (2m42s)
                Queue: EMAIL
                Tries: 1
```

## Watching Tasks Processing

You can view tasks processing through their life cycle using the CLI:

```
$ ajc task watch
[14:08:41] 24YUZF4MzOCLgI7kpwrGtT4lYnS: queue: EMAIL type: email:new tries: 0 state: new
[13:08:41] 24YUZF4MzOCLgI7kpwrGtT4lYnS: queue: EMAIL type: email:new tries: 0 state: active
[13:08:41] 24YUZF4MzOCLgI7kpwrGtT4lYnS: queue: EMAIL type: email:new tries: 1 state: complete
```

## Listing Queues and Tasks

You can see all your queues with some basic statusses:

```
$ ajc queue ls
╭─────────────────────────────────────────────────────────────────────────────╮
│                                 Work Queues                                 │
├─────────┬───────┬───────┬──────────┬───────────┬───────────┬────────────────┤
│ Name    │ Items │ Size  │ Replicas │ Max Tries │ Max Items │ Max Concurrent │
├─────────┼───────┼───────┼──────────┼───────────┼───────────┼────────────────┤
│ EMAIL   │ 0     │ 0 B   │ 1        │ 50        │ unlimited │ 100            │
│ DEFAULT │ 3     │ 489 B │ 1        │ 100       │ unlimited │ 100            │
╰─────────┴───────┴───────┴──────────┴───────────┴───────────┴────────────────╯
```

You can see tasks and their statusses:

```
$ ajc task ls
╭───────────────────────────────────────────────────────────────────────────────────────────────╮
│                                            2 Tasks                                            │
├─────────────────────────────┬───────────┬────────────────────────┬──────────┬─────────┬───────┤
│ ID                          │ Type      │ Created                │ State    │ Queue   │ Tries │
├─────────────────────────────┼───────────┼────────────────────────┼──────────┼─────────┼───────┤
│ 24YUZF4MzOCLgI7kpwrGtT4lYnS │ email:new │ 02 Feb 22 13:04:26 UTC │ complete │ EMAIL   │ 1     │
│ 24YV5AyE6epR8XMpIdIzcppYyBK │ email:new │ 02 Feb 22 13:08:41 UTC │ complete │ EMAIL   │ 1     │
╰─────────────────────────────┴───────────┴────────────────────────┴──────────┴─────────┴───────╯
```

And a general overview:

```
$ ajc info
Tasks Storage:

         Entries: 5 @ 2.2 KiB
    Memory Based: false
        Replicas: 1
  Archive Period: forever
     First Entry: 02 Feb 22 13:01:19 UTC (14m24s)
     Last Update: 02 Feb 22 13:08:41 UTC (7m2s)

DEFAULT Work Queue:

         Entries: 3 @ 489 B
    Memory Based: false
        Replicas: 1
  Archive Period: forever
  Max Task Tries: 100
    Max Run Time: 1m0s
  Max Concurrent: 100
     Max Entries: unlimited
      First Item: 02 Feb 22 13:01:19 UTC (14m24s)
       Last Item: 02 Feb 22 13:02:16 UTC (13m27s)

EMAIL Work Queue:

         Entries: 0 @ 0 B
    Memory Based: false
        Replicas: 1
  Archive Period: forever
  Max Task Tries: 50
    Max Run Time: 1h0m0s
  Max Concurrent: 100
     Max Entries: unlimited
       Last Item: 02 Feb 22 13:08:41 UTC (7m2s)
```
