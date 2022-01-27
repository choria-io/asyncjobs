// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/segmentio/ksuid"
)

type TaskState string

const (
	TaskStateUnknown    = "unknown"
	TaskStateNew        = "new"
	TaskStateActive     = "active"
	TaskStateRetry      = "retry"
	TaskStateExpired    = "expired"
	TaskStateCompleted  = "complete"
	TaskStateQueueError = "queue_error"
)

type Task struct {
	ID          string      `json:"id"`
	Type        string      `json:"type"`
	Queue       string      `json:"queue"`
	Payload     []byte      `json:"payload"`
	Deadline    *time.Time  `json:"deadline,omitempty"`
	Result      *TaskResult `json:"result,omitempty"`
	State       TaskState   `json:"state"`
	CreatedAt   time.Time   `json:"created"`
	LastTriedAt *time.Time  `json:"tried,omitempty"`
	Tries       int         `json:"tries"`
	LastErr     string      `json:"last_err,omitempty"`

	storageOptions interface{}
	mu             sync.Mutex
}

type TaskResult struct {
	Payload     []byte    `json:"payload"`
	CompletedAt time.Time `json:"completed"`
}

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

type TaskOpt func(*Task) error

// TaskDeadline sets an absolute time after which the task should not be handled
func TaskDeadline(deadline time.Time) TaskOpt {
	return func(t *Task) error {
		t.Deadline = &deadline
		return nil
	}
}
