// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
  "context"
  "sync"
  "time"

  "github.com/nats-io/jsm.go/api"
)

// Queue represents a work queue
type Queue struct {
  // Name is a unique name for the work queue, should be in the character range a-zA-Z0-9
  Name string `json:"name"`
  // MaxAge is the absolute longest time an entry can stay in the queue. When not set items will not expire
  MaxAge time.Duration `json:"max_age"`
  // MaxEntries represents the maximum amount of entries that can be in the queue. When it's full new entries will be rejected. When unset no limit is applied.
  MaxEntries int `json:"max_entries"`
  // DiscardOld indicates that when MaxEntries are reached old entries will be discarded rather than new ones rejected
  DiscardOld bool `json:"discard_old"`
  // MaxTries is the maximum amount of times a entry can be tried, entries will be tried every MaxRunTime with some jitter applied. Default to DefaultMaxTries
  MaxTries int `json:"max_tries"`
  // MaxRunTime is the maximum time a task can be processed. Defaults to DefaultJobRunTime
  MaxRunTime time.Duration `json:"max_runtime"`
  // MaxConcurrent is the total number of in-flight tasks across all active task handlers combined. Defaults to DefaultQueueMaxConcurrent
  MaxConcurrent int `json:"max_concurrent"`
  // NoCreate will not try to create a queue, will bind to an existing one or fail
  NoCreate bool
  // MaxBytes is the maximum amount of bytes that can be stored in the queue
  MaxBytes int64 `json:"max_bytes"`

  mu      sync.Mutex
  storage Storage
}

// QueueInfo holds information about a queue state
type QueueInfo struct {
  // Name is the name of the queue
  Name string `json:"name"`
  // Time is the information was gathered
  Time time.Time `json:"time"`
  // Stream is the active JetStream Stream Information
  Stream *api.StreamInfo `json:"stream_info"`
  // Consumer is the worker stream information
  Consumer *api.ConsumerInfo `json:"consumer_info"`
}

func (q *Queue) retryTaskByID(ctx context.Context, id string) error {
  return q.storage.RetryTaskByID(ctx, q, id)
}

func (q *Queue) enqueueTask(ctx context.Context, task *Task) error {
  task.Queue = q.Name
  return q.storage.EnqueueTask(ctx, q, task)
}

func newDefaultQueue() *Queue {
  return &Queue{
    Name:          "DEFAULT",
    MaxRunTime:    time.Minute,
    MaxTries:      100,
    MaxConcurrent: DefaultQueueMaxConcurrent,
    MaxBytes:      DefaultMaxBytes,
    MaxAge:        0,
    DiscardOld:    false,
    mu:            sync.Mutex{},
  }
}
