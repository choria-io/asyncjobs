// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"encoding/json"
	"net/http"
	"runtime/debug"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/choria-io/asyncjobs"
	"github.com/choria-io/asyncjobs/httpserver/internal/gen"
)

// writeJSON marshals v as JSON with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, gen.ErrorErrorCodeInternal, "response marshal failed", "", "")
		return
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// GetHealthz implements gen.ServerInterface for the liveness probe. It does
// not touch storage.
func (s *Server) GetHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, gen.HealthResponse{
		Status: gen.Ok,
		Time:   time.Now().UTC(),
	})
}

// GetReadyz implements gen.ServerInterface for the readiness probe. It pings
// the admin storage to validate that the configuration KV and tasks stream
// are reachable. Both status codes (200 and 503) share the same body shape so
// probes can decode uniformly.
func (s *Server) GetReadyz(w http.ResponseWriter, _ *http.Request) {
	resp := gen.ReadinessResponse{}
	ready := true

	admin := s.client.StorageAdmin()

	_, err := admin.TasksInfo()
	if err != nil {
		ready = false
		e := "tasks storage unreachable"
		resp.Checks.Tasks = gen.CheckStatus{Ok: false, Error: &e}
	} else {
		resp.Checks.Tasks = gen.CheckStatus{Ok: true}
	}

	_, err = admin.ConfigurationInfo()
	if err != nil {
		ready = false
		e := "configuration storage unreachable"
		resp.Checks.Config = gen.CheckStatus{Ok: false, Error: &e}
	} else {
		resp.Checks.Config = gen.CheckStatus{Ok: true}
	}

	resp.Ready = ready
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, resp)
}

// handleOpenAPISpec serves the OpenAPI 3.0 spec as JSON. The YAML is embedded
// at build time and converted once at server boot.
func (s *Server) handleOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(s.openAPIJSON)
}

// GetInfo implements gen.ServerInterface.
func (s *Server) GetInfo(w http.ResponseWriter, _ *http.Request) {
	info := gen.Info{
		Version: serverVersion(),
		Auth:    s.authMode(),
	}

	admin := s.client.StorageAdmin()
	if names, err := admin.QueueNames(); err == nil {
		qc := len(names)
		info.QueueCount = &qc
	}

	if ti, err := admin.TasksInfo(); err == nil && ti != nil && ti.Stream != nil {
		tc := int(ti.Stream.State.Msgs)
		info.TaskCount = &tc
	}

	writeJSON(w, http.StatusOK, info)
}

// ListRetryPolicies implements gen.ServerInterface.
func (s *Server) ListRetryPolicies(w http.ResponseWriter, _ *http.Request) {
	names := asyncjobs.RetryPolicyNames()
	out := struct {
		Policies []gen.RetryPolicy `json:"policies"`
	}{Policies: make([]gen.RetryPolicy, 0, len(names))}

	for _, name := range names {
		p, err := asyncjobs.RetryPolicyLookup(name)
		if err != nil {
			continue
		}
		rp, ok := p.(asyncjobs.RetryPolicy)
		if !ok {
			continue
		}
		intervals := make([]int64, len(rp.Intervals))
		for i, d := range rp.Intervals {
			intervals[i] = int64(d)
		}
		out.Policies = append(out.Policies, gen.RetryPolicy{
			Name:      name,
			Intervals: intervals,
			Jitter:    rp.Jitter,
		})
	}

	writeJSON(w, http.StatusOK, out)
}

// yamlToJSON converts the embedded OpenAPI YAML to JSON. It is called once at
// server construction so handlers serve pre-encoded bytes.
func yamlToJSON(data []byte) ([]byte, error) {
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

// serverVersion returns the Go module version asyncjobs is built with, or
// "dev" when version information is unavailable (e.g. `go run`).
func serverVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	for _, dep := range bi.Deps {
		if dep.Path == "github.com/choria-io/asyncjobs" && dep.Version != "" {
			return dep.Version
		}
	}
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}
