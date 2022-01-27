// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"time"
)

// Queue represents a work queue
type Queue struct {
	// Name is a unique name for the work queue, should be in the character range a-zA-Z0-9
	Name string
	// MaxAge is the absolute longest time an entry can stay in the queue. When not set items will not expire
	MaxAge time.Duration
	// MaxEntries represents the maximum amount of entries that can be in the queue. When it's full new entries will be rejected. When unset no limit is applied.
	MaxEntries int
	// DiscardOld indicates that when MaxEntries are reached old entries will be discarded rather than new ones rejected
	DiscardOld bool
	// Priority is the priority of the queue as expressed in numbers 1-10. A P10 will be polled 10 times every cycle while a P1 will be polled once. Default to DefaultPriority
	Priority int
	// MaxTries is the maximum amount of times a entry can be tried, entries will be tried every MaxRunTime with some jitter applied. Default to DefaultMaxTries
	MaxTries int
	// MaxRunTime is the maximum time a task can be processed. Defaults to DefaultJobRunTime
	MaxRunTime time.Duration
	// MaxConcurrent is the total number of in-flight tasks across all active task handlers combined. Defaults to DefaultQueueMaxConcurrent
	MaxConcurrent int

	storage storage
}

var defaultQueue = Queue{
	Name:          "DEFAULT",
	MaxRunTime:    time.Minute,
	MaxTries:      100,
	MaxConcurrent: DefaultQueueMaxConcurrent,
}

func (q *Queue) enqueueTask(ctx context.Context, task *Task) error {
	task.Queue = q.Name
	return q.storage.EnqueueTask(ctx, q, task)
}
