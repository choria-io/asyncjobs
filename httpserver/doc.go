// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package httpserver exposes the asyncjobs Go client over an HTTP/JSON API.
//
// The contract is defined by api/openapi.yaml; request routing and parameter
// parsing are generated into httpserver/internal/gen/ from that spec.
//
// A Server is constructed with NewServer and started via Run. It enforces
// bearer-token authentication using a static token file (see package
// documentation of Options) and applies fixed HTTP hardening (body-size
// limits, conservative timeouts, dedicated mux).
package httpserver
