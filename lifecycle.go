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

type BaseEvent struct {
	EventID   string `json:"event_id"`
	EventType string `json:"type"`
	TimeStamp int64  `json:"timestamp"`
}

type TaskStateChangeEvent struct {
	TaskID   string        `json:"task_id"`
	State    TaskState     `json:"state"`
	Tries    int           `json:"tries"`
	Queue    string        `json:"queue"`
	TaskType string        `json:"task_type"`
	LastErr  string        `json:"last_error,omitempty"`
	Age      time.Duration `json:"task_age,omitempty"`

	BaseEvent
}

const (
	// TaskStateChangeEventType is the event type for TaskStateChangeEvent types
	TaskStateChangeEventType = "io.choria.asyncjobs.v1.task_state"
)

func ParseEventJSON(event []byte) (interface{}, string, error) {
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
	default:
		return nil, base.EventType, fmt.Errorf("unknown event type %s", base.EventType)
	}
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
			TimeStamp: eid.Time().UnixNano(),
			EventType: TaskStateChangeEventType,
		},
	}

	if !t.CreatedAt.IsZero() {
		e.Age = time.Since(t.CreatedAt)
	}

	return e, nil
}
