// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/choria-io/asyncjobs"
	"github.com/choria-io/asyncjobs/httpserver/internal/gen"
)

// idempotencyTTL is how long a (key → task id) mapping is retained. Chosen to
// match the window documented in the OpenAPI spec.
const idempotencyTTL = 10 * time.Minute

// bulkRetryMax caps the number of ids accepted by POST /v1/tasks/retry. Mirrors
// the spec's documented limit.
const bulkRetryMax = 100

// listTasksMaxLimit is the hard ceiling for GET /v1/tasks regardless of what
// the caller asks for.
const listTasksMaxLimit = 1000

// listTasksDefaultLimit is applied when the query parameter is absent. Matches
// the OpenAPI default.
const listTasksDefaultLimit = 200

// idempotencyStore is a small TTL map keyed by Idempotency-Key. Entries expire
// on access or on insert; we do not run a background sweeper because the map
// is bounded by the unique-key space callers observe over idempotencyTTL.
type idempotencyStore struct {
	mu      sync.Mutex
	entries map[string]idempotencyEntry
}

type idempotencyEntry struct {
	taskID  string
	expires time.Time
}

func newIdempotencyStore() *idempotencyStore {
	return &idempotencyStore{entries: map[string]idempotencyEntry{}}
}

func (s *idempotencyStore) lookup(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[key]
	if !ok {
		return "", false
	}
	if time.Now().After(e.expires) {
		delete(s.entries, key)
		return "", false
	}
	return e.taskID, true
}

func (s *idempotencyStore) store(key, taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for k, e := range s.entries {
		if now.After(e.expires) {
			delete(s.entries, k)
		}
	}
	s.entries[key] = idempotencyEntry{taskID: taskID, expires: now.Add(idempotencyTTL)}
}

// listSemaphore is the capacity guard for GET /v1/tasks. StorageAdmin.Tasks
// creates a fresh ephemeral JetStream consumer per call, so we serialize to
// prevent churn under load. Overflow callers receive 429.
type listSemaphore struct {
	ch chan struct{}
}

func newListSemaphore(capacity int) *listSemaphore {
	return &listSemaphore{ch: make(chan struct{}, capacity)}
}

func (s *listSemaphore) tryAcquire() bool {
	select {
	case s.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *listSemaphore) release() {
	<-s.ch
}

// readCreateTaskBody decodes a TaskCreateRequest and applies the payload
// mutual-exclusion rule. It returns a constructed *asyncjobs.Task ready for
// EnqueueTask on success; on validation failure the response is already
// written and the returned task is nil.
func (s *Server) readCreateTaskBody(w http.ResponseWriter, r *http.Request) (*asyncjobs.Task, bool) {
	var req gen.TaskCreateRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, err.Error(), "invalid_json", "")
		return nil, false
	}

	if req.Payload != nil && req.PayloadBase64 != nil {
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, "payload and payload_base64 are mutually exclusive", "payload_both_set", "payload")
		return nil, false
	}

	opts := []asyncjobs.TaskOpt{}
	if req.Deadline != nil {
		opts = append(opts, asyncjobs.TaskDeadline(*req.Deadline))
	}
	if req.MaxTries != nil {
		opts = append(opts, asyncjobs.TaskMaxTries(*req.MaxTries))
	}
	if req.Dependencies != nil && len(*req.Dependencies) > 0 {
		opts = append(opts, asyncjobs.TaskDependsOnIDs((*req.Dependencies)...))
	}
	if req.LoadDependencies != nil && *req.LoadDependencies {
		opts = append(opts, asyncjobs.TaskRequiresDependencyResults())
	}

	var initialPayload any
	if req.PayloadBase64 == nil {
		initialPayload = req.Payload
	}

	task, err := asyncjobs.NewTask(req.Type, initialPayload, opts...)
	if err != nil {
		writeLibraryError(w, err)
		return nil, false
	}

	if req.PayloadBase64 != nil {
		task.Payload = *req.PayloadBase64
	}
	if req.Signature != nil {
		task.Signature = *req.Signature
	}

	return task, true
}

// CreateTask implements gen.ServerInterface.
func (s *Server) CreateTask(w http.ResponseWriter, r *http.Request, params gen.CreateTaskParams) {
	if params.IdempotencyKey != nil {
		if id, ok := s.idempotency.lookup(*params.IdempotencyKey); ok {
			task, err := s.client.LoadTaskByID(id)
			if err == nil {
				w.Header().Set("Location", "/v1/tasks/"+task.ID)
				writeJSON(w, http.StatusOK, toGenTask(task))
				return
			}
			// fall through — the remembered id is gone, treat as fresh
		}
	}

	task, ok := s.readCreateTaskBody(w, r)
	if !ok {
		return
	}

	if err := s.client.EnqueueTask(r.Context(), task); err != nil {
		writeLibraryError(w, err)
		return
	}

	if params.IdempotencyKey != nil {
		s.idempotency.store(*params.IdempotencyKey, task.ID)
	}

	w.Header().Set("Location", "/v1/tasks/"+task.ID)
	writeJSON(w, http.StatusCreated, toGenTask(task))
}

// GetTask implements gen.ServerInterface.
func (s *Server) GetTask(w http.ResponseWriter, _ *http.Request, id string) {
	task, err := s.client.LoadTaskByID(id)
	if err != nil {
		writeLibraryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toGenTask(task))
}

// DeleteTask implements gen.ServerInterface.
func (s *Server) DeleteTask(w http.ResponseWriter, _ *http.Request, id string) {
	if err := s.client.StorageAdmin().DeleteTaskByID(id); err != nil {
		writeLibraryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RetryTask implements gen.ServerInterface.
func (s *Server) RetryTask(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.client.RetryTaskByID(r.Context(), id); err != nil {
		writeLibraryError(w, err)
		return
	}

	task, err := s.client.LoadTaskByID(id)
	if err != nil {
		writeLibraryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toGenTask(task))
}

// RetryTasks implements gen.ServerInterface.
func (s *Server) RetryTasks(w http.ResponseWriter, r *http.Request) {
	var req gen.BulkRetryRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, err.Error(), "invalid_json", "")
		return
	}
	if len(req.Ids) == 0 {
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, "ids is required", "ids_required", "ids")
		return
	}
	if len(req.Ids) > bulkRetryMax {
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, fmt.Sprintf("ids may not exceed %d entries", bulkRetryMax), "ids_too_many", "ids")
		return
	}

	results := make([]gen.BulkRetryResult, 0, len(req.Ids))
	for _, id := range req.Ids {
		res := gen.BulkRetryResult{Id: id}
		err := s.client.RetryTaskByID(r.Context(), id)
		switch {
		case err == nil:
			res.Status = gen.BulkRetryResultStatusOk
		case errors.Is(err, asyncjobs.ErrTaskNotFound):
			res.Status = gen.BulkRetryResultStatusNotFound
			msg := err.Error()
			res.Error = &msg
		default:
			res.Status = gen.BulkRetryResultStatusFailed
			msg := err.Error()
			res.Error = &msg
		}
		results = append(results, res)
	}

	writeJSON(w, http.StatusOK, gen.BulkRetryResponse{Results: results})
}

// ListTasks implements gen.ServerInterface.
func (s *Server) ListTasks(w http.ResponseWriter, r *http.Request, params gen.ListTasksParams) {
	if !s.listSem.tryAcquire() {
		writeError(w, http.StatusTooManyRequests, gen.ErrorErrorCodeRateLimited, "list tasks in progress, retry later", "list_in_progress", "")
		return
	}
	defer s.listSem.release()

	limit := listTasksDefaultLimit
	if params.Limit != nil {
		if *params.Limit < 1 {
			writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, "limit must be positive", "limit_invalid", "limit")
			return
		}
		limit = *params.Limit
	}
	if limit > listTasksMaxLimit {
		limit = listTasksMaxLimit
	}

	filter := taskFilter{
		queue:        valueOrEmpty(params.Queue),
		taskType:     valueOrEmpty(params.Type),
		createdSince: params.CreatedSince,
	}
	if params.State != nil {
		for _, st := range *params.State {
			filter.states = append(filter.states, asyncjobs.TaskState(st))
		}
	}

	admin := s.client.StorageAdmin()
	ch, err := admin.Tasks(r.Context(), int32(limit))
	if err != nil {
		if errors.Is(err, asyncjobs.ErrNoTasks) {
			if params.Stream != nil && *params.Stream == gen.Ndjson {
				w.Header().Set("Content-Type", contentTypeNDJSON)
				w.WriteHeader(http.StatusOK)
				return
			}
			writeJSON(w, http.StatusOK, gen.TaskListResponse{Tasks: []gen.Task{}, Count: 0})
			return
		}
		writeLibraryError(w, err)
		return
	}

	if params.Stream != nil && *params.Stream == gen.Ndjson {
		s.streamTasksNDJSON(w, r.Context(), ch, filter)
		return
	}

	tasks := make([]gen.Task, 0, limit)
	for task := range ch {
		if task == nil {
			continue
		}
		if !filter.match(task) {
			continue
		}
		tasks = append(tasks, toGenTask(task))
	}
	writeJSON(w, http.StatusOK, gen.TaskListResponse{Tasks: tasks, Count: len(tasks)})
}

// streamTasksNDJSON writes a newline-delimited JSON stream, one task per line.
// The StorageAdmin.Tasks channel closes on its internal 60s timeout; the caller
// may also disconnect at any time via ctx cancellation.
func (s *Server) streamTasksNDJSON(w http.ResponseWriter, ctx context.Context, ch <-chan *asyncjobs.Task, filter taskFilter) {
	w.Header().Set("Content-Type", contentTypeNDJSON)
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)

	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-ch:
			if !ok {
				return
			}
			if task == nil || !filter.match(task) {
				continue
			}
			if err := enc.Encode(toGenTask(task)); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// taskFilter implements the post-fetch filters described in PLAN-STEP-3.md. It
// is safe to call match on a zero-value filter — it returns true.
type taskFilter struct {
	states       []asyncjobs.TaskState
	queue        string
	taskType     string
	createdSince *time.Time
}

func (f *taskFilter) match(t *asyncjobs.Task) bool {
	if len(f.states) > 0 {
		hit := false
		for _, st := range f.states {
			if t.State == st {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if f.queue != "" && t.Queue != f.queue {
		return false
	}
	if f.taskType != "" && t.Type != f.taskType {
		return false
	}
	if f.createdSince != nil && t.CreatedAt.Before(*f.createdSince) {
		return false
	}
	return true
}

func valueOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

// toGenTask converts a library Task to the spec's Task shape. Pointer fields
// are only set when the library value is meaningful; this keeps the wire shape
// matching the spec's optionality.
func toGenTask(t *asyncjobs.Task) gen.Task {
	out := gen.Task{
		Id:      t.ID,
		Type:    t.Type,
		Payload: t.Payload,
		State:   gen.TaskState(t.State),
		Created: t.CreatedAt,
	}
	if t.Queue != "" {
		q := t.Queue
		out.Queue = &q
	}
	if t.Deadline != nil {
		d := *t.Deadline
		out.Deadline = &d
	}
	if len(t.Dependencies) > 0 {
		deps := append([]string(nil), t.Dependencies...)
		out.Dependencies = &deps
	}
	if len(t.DependencyResults) > 0 {
		results := make(map[string]gen.TaskResult, len(t.DependencyResults))
		for k, v := range t.DependencyResults {
			if v == nil {
				continue
			}
			results[k] = gen.TaskResult{Payload: v.Payload, Completed: v.CompletedAt}
		}
		out.DependencyResults = &results
	}
	if t.LoadDependencies {
		ld := t.LoadDependencies
		out.LoadDependencies = &ld
	}
	if t.MaxTries != 0 {
		mt := t.MaxTries
		out.MaxTries = &mt
	}
	if t.Result != nil {
		res := gen.TaskResult{Payload: t.Result.Payload, Completed: t.Result.CompletedAt}
		out.Result = &res
	}
	if t.Signature != "" {
		sig := t.Signature
		out.Signature = &sig
	}
	if t.LastTriedAt != nil {
		tr := *t.LastTriedAt
		out.Tried = &tr
	}
	if t.Tries != 0 {
		tries := t.Tries
		out.Tries = &tries
	}
	if t.LastErr != "" {
		le := t.LastErr
		out.LastErr = &le
	}
	return out
}
