// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"encoding/json"
	"fmt"
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
	// TaskStateExpired tasks that reached their deadline or maximum tries
	TaskStateExpired TaskState = "expired"
	// TaskStateTerminated indicates that the task was terminated via the ErrTerminateTask error
	TaskStateTerminated TaskState = "terminated"
	// TaskStateCompleted tasks that are completed
	TaskStateCompleted TaskState = "complete"
	// TaskStateQueueError tasks that could not have their associated Work Queue item created
	TaskStateQueueError TaskState = "queue_error"
	// TaskStateBlocked tasks that are waiting on dependencies
	TaskStateBlocked TaskState = "blocked"
	// TaskStateUnreachable tasks that could not be run due to dependency problems
	TaskStateUnreachable TaskState = "unreachable"
)

var nameToTaskState = map[string]TaskState{
	string(TaskStateUnknown):     TaskStateUnknown,
	string(TaskStateNew):         TaskStateNew,
	string(TaskStateActive):      TaskStateActive,
	string(TaskStateRetry):       TaskStateRetry,
	string(TaskStateExpired):     TaskStateExpired,
	string(TaskStateTerminated):  TaskStateTerminated,
	string(TaskStateCompleted):   TaskStateCompleted,
	string(TaskStateQueueError):  TaskStateQueueError,
	string(TaskStateBlocked):     TaskStateBlocked,
	string(TaskStateUnreachable): TaskStateUnreachable,

	"completed": TaskStateCompleted, // backward compat and just general UX
}

// Task represents a job item that handlers will execute
type Task struct {
	// ID is a k-sortable unique ID for the task
	ID string `json:"id"`
	// Type is a free form string that can later be used as a routing key to send tasks to handlers
	Type string `json:"type"`
	// Queue is the name of the queue the task was enqueued with, set only during the enqueue operation else empty
	Queue string `json:"queue"`
	// Dependencies are IDs of tasks that should complete before this one becomes unblocked
	Dependencies []string `json:"dependencies,omitempty"`
	// DependentResults are results for dependent tasks
	DependencyResults map[string]*TaskResult `json:"dependency_results,omitempty"`
	// LoadDependencies indicates if this task should load dependency results before execting
	LoadDependencies bool `json:"load_dependencies,omitempty"`
	// Payload is a JSON representation of the associated work
	Payload []byte `json:"payload"`
	// Deadline is a cut-off time for the job to complete, should a job be scheduled after this time it will fail.
	// In-Flight jobs are allowed to continue past this time. Only starting handlers are impacted by this deadline.
	Deadline *time.Time `json:"deadline,omitempty"`
	// MaxTries sets a per task maximum try limit. If this task is in a queue that allow fewer tries the queue max tries
	// will override this setting.  A task may not exceed the work queue max tries
	MaxTries int `json:"max_tries"`
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

	storageOptions any
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
	Payload     any       `json:"payload"`
	CompletedAt time.Time `json:"completed"`
}

// NewTask creates a new task of taskType that can later be used to route tasks to handlers.
// The task will carry a JSON encoded representation of payload.
func NewTask(taskType string, payload any, opts ...TaskOpt) (*Task, error) {
	if !IsValidName(taskType) {
		return nil, fmt.Errorf("%w: must match %s", ErrTaskTypeInvalid, validNameMatcher)
	}

	id, err := ksuid.NewRandom()
	if err != nil {
		return nil, err
	}

	if taskType == "" {
		return nil, ErrTaskTypeRequired
	}

	t := &Task{
		ID:        id.String(),
		Type:      taskType,
		CreatedAt: time.Now().UTC(),
		MaxTries:  DefaultMaxTries,
		State:     TaskStateNew,
	}

	if payload != nil {
		p, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		t.Payload = p
	}

	for _, opt := range opts {
		err = opt(t)
		if err != nil {
			return nil, err
		}
	}

	if len(t.Dependencies) > 0 {
		t.State = TaskStateBlocked
	}

	return t, nil
}

// IsPastDeadline determines if the task is past it's deadline
func (t *Task) IsPastDeadline() bool {
	return t.Deadline != nil && time.Since(*t.Deadline) > 0
}

// HasDependencies determines if the task has any dependencies
func (t *Task) HasDependencies() bool {
	return len(t.Dependencies) > 0
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

// TaskMaxTries sets a maximum to the amount of processing attempts a task will have, the queue
// max tries will override this
func TaskMaxTries(tries int) TaskOpt {
	return func(t *Task) error {
		t.MaxTries = tries
		return nil
	}
}

// TaskDependsOnIDs are IDs that this task is dependent on, can be called multiple times
func TaskDependsOnIDs(ids ...string) TaskOpt {
	return func(t *Task) error {
		var should bool

		for _, id := range ids {
			should = true

			for _, d := range t.Dependencies {
				if d == id {
					should = false
					break
				}
			}

			if should {
				t.Dependencies = append(t.Dependencies, id)
			}
		}

		return nil
	}
}

// TaskDependsOn are Tasks that this task is dependent on, can be called multiple times
func TaskDependsOn(tasks ...*Task) TaskOpt {
	return func(t *Task) error {
		for _, task := range tasks {
			err := TaskDependsOnIDs(task.ID)(t)
			if err != nil {
				return err
			}
		}

		return nil
	}
}

// TaskRequiresDependencyResults indicates that if a task has any dependencies their results should be loaded before execution
func TaskRequiresDependencyResults() TaskOpt {
	return func(t *Task) error {
		t.LoadDependencies = true
		return nil
	}
}
