// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nats-io/jsm.go/api"

	"github.com/choria-io/asyncjobs"
	"github.com/choria-io/asyncjobs/httpserver/internal/gen"
)

// Defaults applied to queues created through the HTTP API. Queue storage
// characteristics (replicas, memory-backing) are not part of the v1 spec, so
// the server picks conservative defaults.
const (
	defaultQueueReplicas = 1
	defaultQueueMemory   = false
)

// CreateQueue implements gen.ServerInterface.
func (s *Server) CreateQueue(w http.ResponseWriter, r *http.Request) {
	var req gen.QueueCreateRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, err.Error(), "invalid_json", "")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, "name is required", "queue_name_required", "name")
		return
	}
	if req.Name == "DEFAULT" && !s.opts.allowCreateDefaultQueue {
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, "DEFAULT queue creation is disabled; start the server with WithAllowCreateDefaultQueue or pick a different name", "default_queue_disallowed", "name")
		return
	}

	q := &asyncjobs.Queue{Name: req.Name}

	if req.MaxAge != nil {
		d, err := time.ParseDuration(*req.MaxAge)
		if err != nil {
			writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, "max_age: "+err.Error(), "invalid_duration", "max_age")
			return
		}
		q.MaxAge = d
	}
	if req.MaxRuntime != nil {
		d, err := time.ParseDuration(*req.MaxRuntime)
		if err != nil {
			writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, "max_runtime: "+err.Error(), "invalid_duration", "max_runtime")
			return
		}
		q.MaxRunTime = d
	}
	if req.MaxEntries != nil {
		q.MaxEntries = *req.MaxEntries
	}
	if req.DiscardOld != nil {
		q.DiscardOld = *req.DiscardOld
	}
	if req.MaxTries != nil {
		q.MaxTries = *req.MaxTries
	}
	if req.MaxConcurrent != nil {
		q.MaxConcurrent = *req.MaxConcurrent
	}

	admin := s.client.StorageAdmin()
	if _, err := admin.QueueInfo(req.Name); err == nil {
		writeError(w, http.StatusConflict, gen.ErrorErrorCodeDuplicate, fmt.Sprintf("queue %q already exists", req.Name), "queue_already_exists", "")
		return
	}

	if err := admin.PrepareQueue(q, defaultQueueReplicas, defaultQueueMemory); err != nil {
		writeLibraryError(w, err)
		return
	}

	nfo, err := admin.QueueInfo(req.Name)
	if err != nil {
		writeLibraryError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, toGenQueueInfo(nfo))
}

// ListQueues implements gen.ServerInterface.
func (s *Server) ListQueues(w http.ResponseWriter, _ *http.Request) {
	qs, err := s.client.StorageAdmin().Queues()
	if err != nil {
		writeLibraryError(w, err)
		return
	}

	out := make([]gen.QueueInfo, 0, len(qs))
	for _, q := range qs {
		if q == nil {
			continue
		}
		out = append(out, toGenQueueInfo(q))
	}
	writeJSON(w, http.StatusOK, struct {
		Queues []gen.QueueInfo `json:"queues"`
	}{Queues: out})
}

// GetQueue implements gen.ServerInterface.
func (s *Server) GetQueue(w http.ResponseWriter, _ *http.Request, name string) {
	nfo, err := s.client.StorageAdmin().QueueInfo(name)
	if err != nil {
		writeLibraryError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toGenQueueInfo(nfo))
}

// PurgeQueue implements gen.ServerInterface.
func (s *Server) PurgeQueue(w http.ResponseWriter, _ *http.Request, name string) {
	if err := s.client.StorageAdmin().PurgeQueue(name); err != nil {
		writeLibraryError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// DeleteQueue implements gen.ServerInterface.
func (s *Server) DeleteQueue(w http.ResponseWriter, _ *http.Request, name string) {
	if err := s.client.StorageAdmin().DeleteQueue(name); err != nil {
		writeLibraryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// toGenQueueInfo converts library QueueInfo into the spec response. Duration
// fields are int64 nanoseconds to match the library's time.Duration JSON
// encoding. Opaque JetStream and consumer detail are intentionally never
// returned; callers needing that granularity use the NATS tooling directly.
func toGenQueueInfo(nfo *asyncjobs.QueueInfo) gen.QueueInfo {
	out := gen.QueueInfo{Name: nfo.Name}

	if !nfo.Time.IsZero() {
		t := nfo.Time
		out.Time = &t
	}

	if nfo.Stream != nil {
		cfg := nfo.Stream.Config
		maxAge := int64(cfg.MaxAge)
		out.MaxAge = &maxAge
		entries := int(cfg.MaxMsgs)
		out.MaxEntries = &entries
		discard := cfg.Discard == api.DiscardOld
		out.DiscardOld = &discard
	}

	if nfo.Consumer != nil {
		cfg := nfo.Consumer.Config
		maxTries := cfg.MaxDeliver
		out.MaxTries = &maxTries
		maxRuntime := int64(cfg.AckWait)
		out.MaxRuntime = &maxRuntime
		maxConcurrent := cfg.MaxAckPending
		out.MaxConcurrent = &maxConcurrent
	}

	return out
}
