+++
title = "Handlers in Docker"
toc = true
weight = 30
+++

We want to make it really easy to run handler services in Docker, toword that version `0.0.4` introduces a packager that can create containers on your behalf.

## Preparing Handlers

### Go Based

The idea is that you would create a Handler per Go package, the packager will then pull in all the configured handlers into a small microservice.

```go
package handler

import (
	aj "github.com/choria-io/asyncjobs"
)

func AsyncJobHandler(ctx context.Context, log aj.Logger, task *aj.Task) (interface{}, error) {
	// process your email
}
```

Place this in any package you like, for example `git.example.com/example/email/new`. You can have many handlers, as long as they are in packages like here.

### Other Languages

Other languages are supported using NATS Request-Reply, implement them according to the protocol describe in [Remote Request-Reply Handlers](../../reference/request-reply/).

## Packaging

In a empty directory create a file `asyncjobs.yaml` with the following content:

```yaml
# The NATS Context to connect with.
#
# Same as NatsContext() client option
nats: AJ_EMAIL

# The Work Queue to consume.
#
# Same as BindWorkQueue() client option
queue: EMAIL

# The package name to generate
name: git.example.com/example

# The version of github.com/choria-io/asyncjobs to use,
# something go get would accept. Defeaults to the same
# as the CLI version
asyncjobs: latest

# Use the RetryLinearTenMinutes retry policy,
#
# Equivelant to client RetryBackoffPolicyName() option
retry: 10m

# Discard tasks that reach complete state.
#
# Same as DiscardTaskStates() client option
discard:
  - completed

# List of Task handlers
tasks:
  - type: email:new
    package: git.example.com/example/email/new
    version: v0.2.0
  - type: audit:log
    remote: true
  - type: webhook:call
    command: webhook/call.sh
```

We set up a `remote: true` Task handler for `audit:log` Tasks, this will delegate to external processes, see [Remote Request Reply Handlers](../../reference/request-reply/).

The `webhook:call` Task handler is a shell script that should exist in `commands/webhook/call.sh`, it will be copied into the container. It's most likely you will need dependencies not in the default container, I suggest using the generated one as a `FROM` container to derive one with your dependencies met via the alpine package system.

Next we create our package:

```
$ ajc package docker
╭────────────────────────────────────────────────────────────────╮
│                 Handler Microservice Settings                  │
├────────────────────────────────┬───────────────────────────────┤
│ Package Name                   │ git.example.com/example       │
│ NATS Context Name              │ AJ_EMAIL                      │
│ Work Queue                     │ EMAIL                         │
│ Task Handlers                  │ 2                             │
│ github.com/choria-io/asyncjobs │ latest                        │
╰────────────────────────────────┴───────────────────────────────╯

╭───────────────────────────────────────────────────────────────────────────────────────────────╮
│                              Handler Configuration and Packages                               │
├──────────────┬───────────────────────┬────────────────────────────────────────────────────────┤
│ Task Type    │ Handler Kind          │ Detail                                                 │
├──────────────┼───────────────────────┼────────────────────────────────────────────────────────┤
│ email:new    │ Go Package            │ git.example.com/example/email/new@v0.2.0               │
│ webhook:call │ External Command      │ webhook/call.sh                                        │
│ audit:log    │ Request-Reply Service │ CHORIA_AJ.H.T.audit:log                                │
╰──────────────┴───────────────────────┴────────────────────────────────────────────────────────╯

Build your container using 'docker build'

$ ls -l
total 12
-rw-rw-r-- 1 rip rip  166 Feb  8 17:48 asyncjobs.yaml
-rw-r--r-- 1 rip rip  713 Feb  8 20:01 Dockerfile
-rw-r--r-- 1 rip rip 2540 Feb  8 20:01 main.go
drwxrwxr-x 3 rip rip   19 Feb  8 17:48 commands
```

You see we have a `main.go` that will be built into a container:

```
$ docker build . --tag example/email:latest
$ docker push example/email:latest
```

## Running

The container will rely on a NATS Context for connectivity options, lets create one in the same directory:

```
$ pwd 
/home/myname/work/email_service
$ XDG_CONFIG_HOME=`pwd` nats context add AJ_EMAIL --server nats://nats.example.net:4222
NATS Configuration Context "AJ_EMAIL"

      Server URLs: nats.example.net:4222
             Path: /home/myname/work/email_service/nats/context/AJ_EMAIL.json
```

We can now run it:

```
$ docker run -ti -v "/home/myname/work/email_service/nats:/handler/config/nats" -p 8080:8080 --rm example/email:latest
INFO[19:07:39] Connecting using Context AJ_EMAIL consuming work queue EMAIL with concurrency 4
WARN[19:07:39] Exposing Prometheus metrics on port 8080
```

Note we mount the `nats` configuration directory into `/handler/config/nats` which is where the container will look for the context configuration.  Should you need other supporting files like credentials you can place them in the container and reference them at their in-container paths.

## Environment Configuration

A few environment variables can be set to influence the container:

| Variable          | Description                                                              | YAML Item |
|-------------------|--------------------------------------------------------------------------|-----------|
| `AJ_WORK_QUEUE`   | The name of the Queue to connect to                                      | `queue`   |
| `AJ_NATS_CONTEXT` | The NATS context to use for connectivity                                 | `nats`    |
| `XDG_CONFIG_HOME` | The prefix for NATS Context configuration, defaults to `/handler/config` |           |
| `AJ_CONCURRENCY`  | The number of workers to run, defaults to `runtime.NumCPU()`             |           |
| `AJ_DEBUG`        | Set to `1` to enable debug logging                                       |           |
| `AJ_RETRY_POLICY` | Sets the Retry Backoff Policy, one of `10m`, `1h`, `1m`, `default`       | `retry`   |
