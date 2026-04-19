// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package gen

import _ "embed"

// OpenAPIYAML is the bundled OpenAPI 3.0.3 spec, copied from api/openapi.yaml
// by go:generate. The httpserver package serves it on /v1/openapi.json after
// YAML→JSON conversion.
//
//go:embed openapi.yaml
var OpenAPIYAML []byte
