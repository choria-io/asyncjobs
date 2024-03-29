// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/segmentio/ksuid"
)

// BaseEvent is present in all event types and can be used to detect the type
type BaseEvent struct {
	EventID   string    `json:"event_id"`
	EventType string    `json:"type"`
	TimeStamp time.Time `json:"timestamp"`
}

// TaskStateChangeEvent notifies that a significant change occurred in a Task
type TaskStateChangeEvent struct {
	BaseEvent

	// TaskID is the ID of the task, use with LoadTaskByID() to access the task
	TaskID string `json:"task_id"`
	// State is the new state of the Task
	State TaskState `json:"state"`
	// Tries is how many times the Task has been processed
	Tries int `json:"tries"`
	// Queue is the queue the task is in, can be empty
	Queue string `json:"queue,omitempty"`
	// TaskType is the task routing type
	TaskType string `json:"task_type"`
	// LstErr is the error that caused a task to change state for error state changes
	LastErr string `json:"last_error,omitempty"`
	// Age is the time since the task was created in milliseconds
	Age time.Duration `json:"task_age,omitempty"`
}

// LeaderElectedEvent notifies that a leader election was won
type LeaderElectedEvent struct {
	BaseEvent

	// Name of the process that gained leadership
	Name string `json:"name"`
	// Component is the component that is reporting
	Component string `json:"component"`
}

const (
	// TaskStateChangeEventType is the event type for TaskStateChangeEvent events
	TaskStateChangeEventType = "io.choria.asyncjobs.v1.task_state"

	// LeaderElectedEventType is the event type for LeaderElectedEvent events
	LeaderElectedEventType = "io.choria.asyncjobs.v1.leader_elected"
)

// ParseEventJSON parses event bytes returning the parsed Event and its event type
func ParseEventJSON(event []byte) (any, string, error) {
	var base BaseEvent
	err := json.Unmarshal(event, &base)
	if err != nil {
		return nil, "", err
	}

	switch base.EventType {
	case TaskStateChangeEventType:
		var e TaskStateChangeEvent
		err := json.Unmarshal(event, &e)
		if err != nil {
			return nil, "", err
		}

		return e, base.EventType, nil

	case LeaderElectedEventType:
		var e LeaderElectedEvent
		err := json.Unmarshal(event, &e)
		if err != nil {
			return nil, "", err
		}

		return e, base.EventType, nil
	default:
		return nil, base.EventType, fmt.Errorf("%w: %s", ErrUnknownEventType, base.EventType)
	}
}

// NewLeaderElectedEvent creates a new event notifying of a leader election win
func NewLeaderElectedEvent(name string, component string) (*LeaderElectedEvent, error) {
	eid, err := ksuid.NewRandom()
	if err != nil {
		return nil, err
	}

	return &LeaderElectedEvent{
		Name:      name,
		Component: component,
		BaseEvent: BaseEvent{
			EventID:   eid.String(),
			TimeStamp: eid.Time().UTC(),
			EventType: LeaderElectedEventType,
		},
	}, nil
}

// NewTaskStateChangeEvent creates a new event notifying of a change in task state
func NewTaskStateChangeEvent(t *Task) (*TaskStateChangeEvent, error) {
	eid, err := ksuid.NewRandom()
	if err != nil {
		return nil, err
	}

	e := &TaskStateChangeEvent{
		TaskID:   t.ID,
		State:    t.State,
		Tries:    t.Tries,
		Queue:    t.Queue,
		TaskType: t.Type,
		LastErr:  t.LastErr,
		BaseEvent: BaseEvent{
			EventID:   eid.String(),
			TimeStamp: eid.Time().UTC(),
			EventType: TaskStateChangeEventType,
		},
	}

	if !t.CreatedAt.IsZero() {
		e.Age = time.Since(t.CreatedAt.Round(time.Millisecond))
	}

	return e, nil
}
