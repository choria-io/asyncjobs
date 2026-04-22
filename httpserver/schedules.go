// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/choria-io/asyncjobs"
	"github.com/choria-io/asyncjobs/httpserver/internal/gen"
)

// scheduleListTimeout bounds how long ListSchedules waits on the config bucket
// watcher. The library's ScheduledTasks call drains KV updates until the
// watcher reports an empty update, so we need a ceiling to avoid hanging when
// that signal is lost.
const scheduleListTimeout = 10 * time.Second

// CreateSchedule implements gen.ServerInterface.
func (s *Server) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	var req gen.ScheduleCreateRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, err.Error(), "invalid_json", "")
		return
	}
	if req.Payload != nil && req.PayloadBase64 != nil {
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, "payload and payload_base64 are mutually exclusive", "payload_both_set", "payload")
		return
	}

	var taskOpts []asyncjobs.TaskOpt
	if req.MaxTries != nil {
		taskOpts = append(taskOpts, asyncjobs.TaskMaxTries(*req.MaxTries))
	}
	if req.DeadlineOffset != nil {
		d, err := time.ParseDuration(*req.DeadlineOffset)
		if err != nil {
			writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, "deadline_offset: "+err.Error(), "invalid_duration", "deadline_offset")
			return
		}
		// The library stores the offset derived from task.Deadline; compute an
		// absolute deadline here so newScheduledTaskFromTask can round-trip it
		// into ScheduledTask.Deadline.
		taskOpts = append(taskOpts, asyncjobs.TaskDeadline(time.Now().Add(d)))
	}

	var initialPayload any
	if req.PayloadBase64 == nil {
		initialPayload = req.Payload
	}

	task, err := asyncjobs.NewTask(req.TaskType, initialPayload, taskOpts...)
	if err != nil {
		writeLibraryError(w, err)
		return
	}
	if req.PayloadBase64 != nil {
		task.Payload = *req.PayloadBase64
	}

	if err := s.client.NewScheduledTask(req.Name, req.Schedule, req.Queue, task); err != nil {
		writeLibraryError(w, err)
		return
	}

	st, err := s.client.LoadScheduledTaskByName(req.Name)
	if err != nil {
		writeLibraryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toGenSchedule(st))
}

// ListSchedules implements gen.ServerInterface.
func (s *Server) ListSchedules(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), scheduleListTimeout)
	defer cancel()

	tasks, err := s.client.ScheduledTasksStorage().ScheduledTasks(ctx)
	if err != nil {
		writeLibraryError(w, err)
		return
	}

	out := make([]gen.Schedule, 0, len(tasks))
	for _, st := range tasks {
		if st == nil {
			continue
		}
		out = append(out, toGenSchedule(st))
	}
	writeJSON(w, http.StatusOK, struct {
		Schedules []gen.Schedule `json:"schedules"`
	}{Schedules: out})
}

// GetSchedule implements gen.ServerInterface.
func (s *Server) GetSchedule(w http.ResponseWriter, _ *http.Request, name string) {
	st, err := s.client.LoadScheduledTaskByName(name)
	if err != nil {
		writeLibraryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toGenSchedule(st))
}

// DeleteSchedule implements gen.ServerInterface.
func (s *Server) DeleteSchedule(w http.ResponseWriter, _ *http.Request, name string) {
	if err := s.client.RemoveScheduledTask(name); err != nil {
		writeLibraryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// toGenSchedule converts a ScheduledTask into the spec response shape. Payload
// and deadline preserve the library's JSON encoding (base64 bytes, int64
// nanosecond duration) so callers can round-trip them verbatim.
func toGenSchedule(st *asyncjobs.ScheduledTask) gen.Schedule {
	out := gen.Schedule{
		Name:      st.Name,
		Schedule:  st.Schedule,
		Queue:     st.Queue,
		TaskType:  st.TaskType,
		CreatedAt: st.CreatedAt,
	}
	if len(st.Payload) > 0 {
		pl := append([]byte(nil), st.Payload...)
		out.Payload = &pl
	}
	if st.Deadline != 0 {
		d := int64(st.Deadline)
		out.Deadline = &d
	}
	if st.MaxTries != 0 {
		mt := st.MaxTries
		out.MaxTries = &mt
	}
	return out
}
