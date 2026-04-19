+++
title = "HTTP API"
description = "Use asyncjobs over HTTP via the ajc server subcommand"
toc = true
weight = 70
+++

The asyncjobs library can be exposed over an HTTP/JSON API so non-Go services and operators can enqueue tasks, manage queues, and inspect schedules without linking the library. The contract is OpenAPI 3.0; the canonical document lives at [`api/openapi.yaml`](https://github.com/choria-io/asyncjobs/blob/main/api/openapi.yaml).

The server is shipped as the `httpserver` subpackage and a launchable `ajc server run` subcommand.

## Quickstart

Start the server on loopback and call it with `curl`:

```
$ ajc queue add EMAIL --run-time 1h
$ ajc server run --queue EMAIL
INFO[0000] asyncjobs HTTP API listening on 127.0.0.1:8080 (none)
```

```
$ curl -sf http://127.0.0.1:8080/v1/info
{"version":"v0.1.0","auth":"none","queue_count":1,"task_count":0}
```

## `ajc server run` flags

| Flag                     | Default          | Notes                                                            |
|--------------------------|------------------|------------------------------------------------------------------|
| `--bind`                 | `127.0.0.1:8080` | Listen address. Non-loopback requires `--unsafe-bind` or mTLS.   |
| `--tls-cert`, `--tls-key`| _(none)_         | Enable TLS. Both must be supplied together.                      |
| `--tls-client-ca`        | _(none)_         | CA bundle used to verify client certificates. Enables mTLS.      |
| `--queue`                | _(none)_         | Work queue the API enqueues to. Required unless `--allow-create-default`. |
| `--allow-create-default` | `false`          | Permit auto-creating the implicit DEFAULT queue.                 |
| `--read-timeout`         | `30s`            | Per-request read timeout.                                        |
| `--write-timeout`        | `30s`            | Per-request write timeout.                                       |
| `--max-body`             | `524288`         | Maximum request body size, in bytes (default 512 KiB).           |
| `--unsafe-bind`          | `false`          | Acknowledge that a non-loopback bind exposes an unauthenticated server. |

The server binds to a dedicated `http.ServeMux`, never the default mux, and applies a `MaxBytesReader` to every request body. Request bodies and the `Authorization` header are never logged.

## Authentication

The server performs no authentication and no authorization. The default loopback bind is sufficient for same-host integrations; anything else must be fronted by a reverse proxy or secured with mTLS.

A non-loopback `--bind` is rejected at startup unless one of the following holds:

- **Reverse proxy** (nginx, oauth2-proxy, envoy, caddy, Traefik, Tailscale, ...): pass `--unsafe-bind` to acknowledge that you have fronted the server with a proxy that terminates authentication.
- **mTLS**: pass `--tls-cert`, `--tls-key`, and `--tls-client-ca`. When a client-CA bundle is configured, every TLS client must present a certificate chain that verifies against it. Certificate subjects are **not** mapped to scopes; successful verification alone grants full access.

`/v1/info` advertises the mode as `auth: "none"` or `auth: "mtls"` so callers and health checks can assert it externally.

### Reverse-proxy requirements

Any fronting proxy must:

- Terminate authentication (bearer token, OIDC, SSO, mTLS, whatever fits) before reaching the asyncjobs listener.
- Preserve or strip request headers per the proxy's own policy; asyncjobs itself reads none.
- Pass through `413 Payload Too Large` and `429 Too Many Requests` emitted by the backend.

Client requests rejected by the proxy receive the proxy's error envelope (a `401` or `403`), not the asyncjobs envelope — those status codes do not originate from this server.

### Rotating mTLS material

There is no hot-reload for TLS or client-CA material; restart the server to pick up new files.

## Endpoint catalog

All paths are versioned under `/v1`.

### Meta

| Method | Path                | Notes                                                    |
|--------|---------------------|----------------------------------------------------------|
| GET    | `/healthz`          | Liveness probe. Does not touch storage.                  |
| GET    | `/readyz`           | Readiness probe. Returns the same body shape on 200/503. |
| GET    | `/v1/openapi.json`  | The full OpenAPI document, generated from the embedded YAML. |
| GET    | `/v1/info`          | Server version, auth mode, queue count, task count.      |
| GET    | `/v1/retry-policies`| The known retry policies and their intervals.            |

### Tasks

| Method | Path                          | Notes                                                    |
|--------|-------------------------------|----------------------------------------------------------|
| POST   | `/v1/tasks`                   | Enqueue a task. Honors `Idempotency-Key`.                |
| GET    | `/v1/tasks`                   | Snapshot list. Supports `limit`, `state`, `queue`, `type`, `created_since`, `stream=ndjson`. Rate-limited. |
| GET    | `/v1/tasks/{id}`              | Fetch a task.                                            |
| DELETE | `/v1/tasks/{id}`              | Remove a task from storage.                              |
| POST   | `/v1/tasks/{id}/retry`        | Retry a single task by id.                               |
| POST   | `/v1/tasks/retry`             | Bulk retry, capped at 100 ids; returns per-item results. |

### Queues

| Method | Path                              | Notes                                                    |
|--------|-----------------------------------|----------------------------------------------------------|
| POST   | `/v1/queues`                      | Create a queue.                                          |
| GET    | `/v1/queues`                      | List queues. Raw JetStream and consumer detail are never returned. |
| GET    | `/v1/queues/{name}`               | Queue detail.                                            |
| DELETE | `/v1/queues/{name}`               | Delete a queue.                                          |
| POST   | `/v1/queues/{name}/purge`         | Purge all entries.                                       |

### Schedules

| Method | Path                       | Notes                            |
|--------|----------------------------|----------------------------------|
| POST   | `/v1/schedules`            | Create a scheduled task.         |
| GET    | `/v1/schedules`            | List schedules.                  |
| GET    | `/v1/schedules/{name}`     | Schedule detail.                 |
| DELETE | `/v1/schedules/{name}`     | Remove a schedule.               |

## Error envelope

Every error response shares the same shape:

```json
{
  "error": {
    "code": "not_found",
    "message": "task not found: 24YUZF...",
    "details": {
      "reason": "task_not_found"
    }
  }
}
```

`code` is a stable string drawn from a closed enum (`invalid_argument`, `not_found`, `conflict`, `duplicate`, `rate_limited`, `payload_too_large`, `unavailable`, `signature_required`, `signature_invalid`, `dependency_failed`, `internal`). Treat `code` as the machine-readable surface; `message` is for operators.

`details.reason` carries a library-specific identifier (e.g. `task_not_found`, `queue_already_exists`, `payload_both_set`) and lets clients branch on a finer-grained signal than `code` alone provides. `details.field` is set on validation failures to the offending field name.

## Quirks worth remembering

**Payload encoding.** Create requests accept either `payload` (any JSON value, server-encoded) or `payload_base64` (pre-encoded bytes). Setting both yields HTTP 400 with `details.reason: payload_both_set`. Task and schedule responses always return `payload` as a base64 string; this matches the library's `[]byte` JSON encoding. Decode before use, and use `payload_base64` when round-tripping a fetched task.

**Duration vs RFC3339 dates.** Queue duration fields (`max_age`, `max_runtime`) accept Go duration strings (`30s`, `5m`) on requests but echo int64 nanoseconds on responses. Task `deadline` is an absolute RFC3339 timestamp. Schedule `deadline_offset` is a Go duration string applied at fire-time. The names differ deliberately so the schemas cannot silently collide.

**Rate-limited list.** `GET /v1/tasks` opens a fresh ephemeral JetStream consumer per call and is serialized server-side. Concurrent callers receive HTTP 429 (`code: "rate_limited"`). `stream=ndjson` tail mode is capped at roughly 60 seconds by the underlying library; clients should reconnect.

**Default-queue guard.** `POST /v1/queues` rejects `name: "DEFAULT"` unless the server was started with `ajc server run --allow-create-default`. The library would otherwise auto-create it, masking misconfigured deployments.

**Signing is the caller's responsibility.** When the deployment has a `TaskVerificationKey` configured, the HTTP server does not sign on the caller's behalf. Callers must supply a pre-computed `signature` field.

**Idempotency.** `POST /v1/tasks` honors the `Idempotency-Key` header for a 10-minute window: a repeat request with the same key returns the existing task as 200 with the same `Location` header rather than enqueuing twice.

## Generating client SDKs

The OpenAPI document at `/v1/openapi.json` is OpenAPI 3.0.3 — emitted from the embedded YAML at server boot. Use a generator that supports 3.0:

```
openapi-generator-cli generate \
  -i http://127.0.0.1:8080/v1/openapi.json \
  -g python \
  -o ./client
```

For the `openapi-generator-cli` tool, pin the input version with `--spec-version 3.0.3` if your generator defaults to 3.1; the asyncjobs spec does not use 3.1-only constructs but generators may emit warnings otherwise.

The repository also exposes the YAML directly at [`api/openapi.yaml`](https://github.com/choria-io/asyncjobs/blob/main/api/openapi.yaml) for offline generation.
