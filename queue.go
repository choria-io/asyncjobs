// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"sync"
	"time"
)

type Queue struct {
	Name          string
	MaxAge        time.Duration
	MaxEntries    int
	DiscardOld    bool // when MaxEntries is reached
	Priority      int
	MaxTries      int
	MaxRunTime    time.Duration
	MaxConcurrent int

	storage Storage
	mu      sync.Mutex
}

var defaultQueue = Queue{
	Name:          "DEFAULT",
	MaxRunTime:    time.Minute,
	MaxTries:      100,
	MaxConcurrent: DefaultQueueMaxConcurrent,
}

func (q *Queue) EnqueueTask(ctx context.Context, task *Task) error {
	task.Queue = q.Name
	return q.storage.EnqueueTask(ctx, q, task)
}
