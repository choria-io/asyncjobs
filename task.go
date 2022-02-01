// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/nats-io/jsm.go/api"
	"github.com/segmentio/ksuid"
)

// TaskState indicates the current state a task is in
type TaskState string

const (
	// TaskStateUnknown is for tasks that do not have a state set
	TaskStateUnknown TaskState = ""
	// TaskStateNew newly created tasks that have not been handled yet
	TaskStateNew TaskState = "new"
	// TaskStateActive tasks that are currently being handled
	TaskStateActive TaskState = "active"
	// TaskStateRetry tasks that previously failed and are waiting retry
	TaskStateRetry TaskState = "retry"
	// TaskStateExpired tasks that reached their deadline
	TaskStateExpired TaskState = "expired"
	// TaskStateTerminated indicates that the task was terminated via the ErrTerminateTask error
	TaskStateTerminated TaskState = "terminated"
	// TaskStateCompleted tasks that are completed
	TaskStateCompleted TaskState = "complete"
	// TaskStateQueueError tasks that could not have their associated Work Queue item created
	TaskStateQueueError TaskState = "queue_error"
)

// Task represents a job item that handlers will execute
type Task struct {
	// ID is a k-sortable unique ID for the task
	ID string `json:"id"`
	// Type is a free form string that can later be used as a routing key to send tasks to handlers
	Type string `json:"type"`
	// Queue is the name of the queue the task was enqueued with, set only during the enqueue operation else empty
	Queue string `json:"queue"`
	// Payload is a JSON representation of the associated work
	Payload []byte `json:"payload"`
	// Deadline is a cut-off time for the job to complete, should a job be scheduled after this time it will fail.
	// In-Flight jobs are allowed to continue past this time. Only starting handlers are impacted by this deadline.
	Deadline *time.Time `json:"deadline,omitempty"`
	// Result is the outcome of the job, only set for successful jobs
	Result *TaskResult `json:"result,omitempty"`
	// State is the most recent recorded state the job is in
	State TaskState `json:"state"`
	// CreatedAt is the time the job was created in UTC timezone
	CreatedAt time.Time `json:"created"`
	// LastTriedAt is a time stamp for when the job was last handed to a handler
	LastTriedAt *time.Time `json:"tried,omitempty"`
	// Tries is how many times the job was handled
	Tries int `json:"tries"`
	// LastErr is the most recent handling error if any
	LastErr string `json:"last_err,omitempty"`

	storageOptions interface{}
	mu             sync.Mutex
}

// TasksInfo is state about the tasks store
type TasksInfo struct {
	// Time is the information was gathered
	Time time.Time `json:"time"`
	// Stream is the active JetStream Stream Information
	Stream *api.StreamInfo `json:"stream_info"`
}

// TaskResult is the result of task execution, this will only be set for successfully processed jobs
type TaskResult struct {
	Payload     interface{} `json:"payload"`
	CompletedAt time.Time   `json:"completed"`
}

// NewTask creates a new task of taskType that can later be used to route tasks to handlers.
// The task will carry a JSON encoded representation of payload.
func NewTask(taskType string, payload interface{}, opts ...TaskOpt) (*Task, error) {
	p, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	id, err := ksuid.NewRandom()
	if err != nil {
		return nil, err
	}

	t := &Task{
		ID:        id.String(),
		Type:      taskType,
		Payload:   p,
		CreatedAt: time.Now().UTC(),
		State:     TaskStateNew,
	}

	for _, opt := range opts {
		err = opt(t)
		if err != nil {
			return nil, err
		}
	}

	return t, nil
}

// TaskOpt configures Tasks made using NewTask()
type TaskOpt func(*Task) error

// TaskDeadline sets an absolute time after which the task should not be handled
func TaskDeadline(deadline time.Time) TaskOpt {
	return func(t *Task) error {
		t.Deadline = &deadline
		return nil
	}
}
