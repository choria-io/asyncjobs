// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// ScheduledTask represents a cron like schedule and task properties that will
// result in regular new tasks to be created machine schedule
type ScheduledTask struct {
	// Name is a unique name for the scheduled task
	Name string `json:"name"`
	// Schedule is a cron specification for the schedule
	Schedule string `json:"schedule"`
	// Queue is the name of a queue to enqueue the task into
	Queue string `json:"queue"`
	// TaskType is the type of task to create
	TaskType string `json:"task_type"`
	// Payload is the task payload for the enqueued tasks
	Payload []byte `json:"payload"`
	// Deadline is the time after scheduling that the deadline would be
	Deadline time.Duration `json:"deadline,omitempty"`
	// MaxTries is how many times the created task could be tried
	MaxTries int `json:"max_tries"`
	// CreatedAt is when the schedule was created
	CreatedAt time.Time `json:"created_at"`
}

type ScheduleWatchEntry struct {
	Name   string
	Task   *ScheduledTask
	Delete bool
}

func newScheduledTaskFromTask(name string, schedule string, queue string, task *Task) (*ScheduledTask, cron.Schedule, error) {
	if name == "" {
		return nil, nil, ErrScheduleNameIsRequired
	}
	if !IsValidName(name) {
		return nil, nil, fmt.Errorf("%w: must match %s", ErrScheduleNameInvalid, validNameMatcher.String())
	}
	if schedule == "" {
		return nil, nil, ErrScheduleIsRequired
	}
	if queue == "" {
		return nil, nil, ErrQueueNameRequired
	}

	cs, err := cron.ParseStandard(schedule)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrScheduleInvalid, err)
	}

	sched := &ScheduledTask{
		Name:      name,
		Schedule:  schedule,
		Queue:     queue,
		TaskType:  task.Type,
		Payload:   task.Payload,
		MaxTries:  task.MaxTries,
		CreatedAt: time.Now().UTC(),
	}

	if task.Deadline != nil {
		sched.Deadline = time.Until(*task.Deadline).Round(time.Second)
		if sched.Deadline < ShortedScheduledDeadline {
			return nil, nil, ErrScheduledTaskShortDeadline
		}
	}

	return sched, cs, nil
}

func newScheduledTask(name string, schedule string, queue string, taskType string, payload interface{}, opts ...TaskOpt) (*ScheduledTask, cron.Schedule, error) {
	task, err := NewTask(taskType, payload, opts...)
	if err != nil {
		return nil, nil, err
	}

	return newScheduledTaskFromTask(name, schedule, queue, task)
}
