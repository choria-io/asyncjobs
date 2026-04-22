# asyncjobs HTTP API

The OpenAPI 3.0 contract for the asyncjobs HTTP API lives in
[`openapi.yaml`](openapi.yaml). It is the source of truth: server
interface code under `httpserver/internal/gen/` is regenerated from it.

## Authentication

The server performs no authentication and no authorization. Deployers
front it with a reverse proxy (nginx, oauth2-proxy, envoy, caddy,
Traefik, Tailscale, ...) that terminates authentication before requests
reach the listener, or configure mTLS with a client-CA bundle so the TLS
handshake itself authenticates the client.

- Default bind is `127.0.0.1:8080`. A non-loopback bind is refused at
  startup unless the server is either configured with a client CA
  (mTLS) or started with explicit acknowledgement that a reverse proxy
  fronts it (`--unsafe-bind` on the CLI,
  `WithUnauthenticatedExposure()` in code).
- `WithTLS` (or `--tls-cert` / `--tls-key`) provides transport
  encryption only, not authentication.
- `WithClientCA` (or `--tls-client-ca`) adds mTLS: every TLS client
  must present a certificate chain that verifies against the supplied
  CA bundle. Subject mapping to scopes is deliberately not performed —
  successful verification alone grants full access.

`GET /v1/info` returns an `auth` field (`"none"` or `"mtls"`) so an
operator can externally assert the server's authentication mode.

`401 Unauthorized` and `403 Forbidden` responses, when a client sees
them, are returned by the fronting proxy and follow the proxy's own
envelope — not the `Error` schema in this spec.

## Browse the spec

```
abt docs serve-api
```

Then open http://localhost:8081. Requires Docker.

## Regenerate server bindings

After editing `openapi.yaml`:

```
abt gen
```

Commit both `openapi.yaml` and the regenerated `*.gen.go` files.
