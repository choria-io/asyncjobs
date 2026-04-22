// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/choria-io/asyncjobs"
	"github.com/choria-io/asyncjobs/httpserver/internal/gen"
)

const contentTypeJSON = "application/json; charset=utf-8"
const contentTypeNDJSON = "application/x-ndjson"

// writeError emits the shared Error envelope. code is the closed generic
// enum value; reason carries a library-specific identifier (may be empty).
// field is non-empty for validation errors and populates details.field.
func writeError(w http.ResponseWriter, status int, code gen.ErrorErrorCode, message, reason, field string) {
	var details *struct {
		Field  *string `json:"field,omitempty"`
		Reason *string `json:"reason,omitempty"`
	}
	if reason != "" || field != "" {
		details = &struct {
			Field  *string `json:"field,omitempty"`
			Reason *string `json:"reason,omitempty"`
		}{}
		if reason != "" {
			r := reason
			details.Reason = &r
		}
		if field != "" {
			f := field
			details.Field = &f
		}
	}

	env := gen.Error{}
	env.Error.Code = code
	env.Error.Message = message
	env.Error.Details = details

	body, err := json.Marshal(env)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeLibraryError maps a library error to the HTTP response shape defined in
// PLAN-STEP-3.md. Unknown errors become 500 with code=internal so callers still
// see a structured envelope.
func writeLibraryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, asyncjobs.ErrTaskNotFound):
		writeError(w, http.StatusNotFound, gen.ErrorErrorCodeNotFound, err.Error(), "task_not_found", "")
	case errors.Is(err, asyncjobs.ErrTaskTypeInvalid), errors.Is(err, asyncjobs.ErrTaskTypeRequired):
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, err.Error(), "task_type_invalid", "")
	case errors.Is(err, asyncjobs.ErrTaskIDInvalid):
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, err.Error(), "task_id_invalid", "")
	case errors.Is(err, asyncjobs.ErrTaskTypeCannotEnqueue):
		writeError(w, http.StatusConflict, gen.ErrorErrorCodeConflict, err.Error(), "task_cannot_enqueue", "")
	case errors.Is(err, asyncjobs.ErrTaskPastDeadline):
		writeError(w, http.StatusConflict, gen.ErrorErrorCodeConflict, err.Error(), "task_past_deadline", "")
	case errors.Is(err, asyncjobs.ErrTaskAlreadyActive):
		writeError(w, http.StatusConflict, gen.ErrorErrorCodeConflict, err.Error(), "task_already_active", "")
	case errors.Is(err, asyncjobs.ErrTaskAlreadyInState):
		writeError(w, http.StatusConflict, gen.ErrorErrorCodeConflict, err.Error(), "task_already_in_state", "")
	case errors.Is(err, asyncjobs.ErrTaskAlreadySigned):
		writeError(w, http.StatusConflict, gen.ErrorErrorCodeConflict, err.Error(), "task_already_signed", "")
	case errors.Is(err, asyncjobs.ErrTaskNotSigned):
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeSignatureRequired, err.Error(), "task_not_signed", "")
	case errors.Is(err, asyncjobs.ErrTaskSignatureInvalid):
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeSignatureInvalid, err.Error(), "task_signature_invalid", "")
	case errors.Is(err, asyncjobs.ErrStorageNotReady), errors.Is(err, asyncjobs.ErrNoNatsConn):
		writeError(w, http.StatusServiceUnavailable, gen.ErrorErrorCodeUnavailable, err.Error(), "storage_not_ready", "")
	case errors.Is(err, asyncjobs.ErrQueueNotFound), errors.Is(err, asyncjobs.ErrQueueConsumerNotFound):
		writeError(w, http.StatusNotFound, gen.ErrorErrorCodeNotFound, err.Error(), "queue_not_found", "")
	case errors.Is(err, asyncjobs.ErrQueueNameRequired):
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, err.Error(), "queue_name_required", "")
	case errors.Is(err, asyncjobs.ErrInvalidQueueState):
		writeError(w, http.StatusServiceUnavailable, gen.ErrorErrorCodeUnavailable, err.Error(), "queue_state_invalid", "")
	case errors.Is(err, asyncjobs.ErrScheduledTaskNotFound):
		writeError(w, http.StatusNotFound, gen.ErrorErrorCodeNotFound, err.Error(), "schedule_not_found", "")
	case errors.Is(err, asyncjobs.ErrScheduledTaskAlreadyExist):
		writeError(w, http.StatusConflict, gen.ErrorErrorCodeDuplicate, err.Error(), "schedule_already_exists", "")
	case errors.Is(err, asyncjobs.ErrDuplicateItem):
		writeError(w, http.StatusConflict, gen.ErrorErrorCodeDuplicate, err.Error(), "duplicate_item", "")
	case errors.Is(err, asyncjobs.ErrScheduleInvalid),
		errors.Is(err, asyncjobs.ErrScheduleNameInvalid),
		errors.Is(err, asyncjobs.ErrScheduleNameIsRequired),
		errors.Is(err, asyncjobs.ErrScheduleIsRequired),
		errors.Is(err, asyncjobs.ErrScheduledTaskShortDeadline):
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, err.Error(), "schedule_invalid", "")
	case errors.Is(err, asyncjobs.ErrUnknownRetryPolicy):
		writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, err.Error(), "unknown_retry_policy", "")
	default:
		writeError(w, http.StatusInternalServerError, gen.ErrorErrorCodeInternal, err.Error(), "", "")
	}
}
