// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package gen holds the OpenAPI-generated server interface and types
// for the asyncjobs HTTP API. Regenerate with:
//
//	go generate ./httpserver/...
package gen

//go:generate cp ../../../api/openapi.yaml openapi.yaml
//go:generate go tool oapi-codegen -config oapi-types.yaml ../../../api/openapi.yaml
//go:generate go tool oapi-codegen -config oapi-server.yaml ../../../api/openapi.yaml
