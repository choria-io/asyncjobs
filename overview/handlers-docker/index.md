# Handlers in Docker

Version `0.0.4` introduces a packager that builds handler containers from configuration.

## Preparing handlers

### Go based

One handler per Go package is recommended. The packager pulls configured handlers into a small microservice.

```go
package handler

import (
	aj "github.com/choria-io/asyncjobs"
)

func AsyncJobHandler(ctx context.Context, log aj.Logger, task *aj.Task) (any, error) {
	// process your email
}
```

Place this in any package, for example `git.example.com/example/email/new`. Multiple handlers are supported as long as each resides in its own package.

### Other languages

Other languages are supported through NATS Request-Reply. Implement them against the protocol described in [Remote Request-Reply Handlers](../../reference/request-reply/).

## Packaging

In an empty directory, create a file `asyncjobs.yaml` with the following content:

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
# something go get would accept. Defaults to the same
# as the CLI version
asyncjobs: latest

# Use the RetryLinearTenMinutes retry policy,
#
# Equivalent to client RetryBackoffPolicyName() option
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

The `audit:log` handler uses `remote: true` and delegates to external processes. See [Remote Request-Reply Handlers](../../reference/request-reply/).

The `webhook:call` handler is a shell script that must exist at `commands/webhook/call.sh` and is copied into the container. Handlers often need dependencies absent from the default container. Use the generated container as a `FROM` base and add dependencies through the alpine package system.

Generate the package:

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

A `main.go` is generated and built into a container:

```
$ docker build . --tag example/email:latest
$ docker push example/email:latest
```

## Running

The container relies on a NATS Context for connectivity. Create one in the same directory:

```
$ pwd 
/home/myname/work/email_service
$ XDG_CONFIG_HOME=`pwd` nats context add AJ_EMAIL --server nats://nats.example.net:4222
NATS Configuration Context "AJ_EMAIL"

      Server URLs: nats.example.net:4222
             Path: /home/myname/work/email_service/nats/context/AJ_EMAIL.json
```

Run the container:

```
$ docker run -ti -v "/home/myname/work/email_service/nats:/handler/config/nats" -p 8080:8080 --rm example/email:latest
INFO[19:07:39] Connecting using Context AJ_EMAIL consuming work queue EMAIL with concurrency 4
WARN[19:07:39] Exposing Prometheus metrics on port 8080
```

The `nats` configuration directory is mounted to `/handler/config/nats`, where the container looks for the context. Additional files such as credentials can be placed in the container and referenced at their in-container paths.

## Environment configuration

The following environment variables influence the container:

| Variable          | Description                                                              | YAML Item |
|-------------------|--------------------------------------------------------------------------|-----------|
| `AJ_WORK_QUEUE`   | The name of the queue to connect to                                      | `queue`   |
| `AJ_NATS_CONTEXT` | The NATS context to use for connectivity                                 | `nats`    |
| `XDG_CONFIG_HOME` | The prefix for NATS context configuration, defaults to `/handler/config` |           |
| `AJ_CONCURRENCY`  | The number of workers to run, defaults to `runtime.NumCPU()`             |           |
| `AJ_DEBUG`        | Set to `1` to enable debug logging                                       |           |
| `AJ_RETRY_POLICY` | Sets the retry backoff policy, one of `10m`, `1h`, `1m`, `default`       | `retry`   |
