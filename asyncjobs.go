// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"regexp"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/nats.go"
)

const (
	// ShortedScheduledDeadline is the shortest deadline a scheduled task may have
	ShortedScheduledDeadline = 30 * time.Second
	// DefaultJobRunTime when not configured for a queue this is the default run-time handlers will get
	DefaultJobRunTime = time.Hour
	// DefaultMaxTries when not configured for a task this is the default tries it will get
	DefaultMaxTries = 10
	// DefaultQueueMaxConcurrent when not configured for a queue this is the default concurrency setting
	DefaultQueueMaxConcurrent = 100
	// DefaultMaxBytes when not configured for a queue defaults to 10Mb
	DefaultMaxBytes = 10485760
)

// StorageAdmin is helpers to support the CLI mainly, this leaks a bunch of details about JetStream
// but that's ok, we're not really intending to change the storage or support more
type StorageAdmin interface {
	Queues() ([]*QueueInfo, error)
	QueueNames() ([]string, error)
	QueueInfo(name string) (*QueueInfo, error)
	PurgeQueue(name string) error
	DeleteQueue(name string) error
	PrepareQueue(q *Queue, replicas int, memory bool) error
	ConfigurationInfo() (*nats.KeyValueBucketStatus, error)
	PrepareConfigurationStore(memory bool, replicas int, maxBytes int64, maxBytesSet bool) error
	PrepareTasks(memory bool, replicas int, retention time.Duration, maxBytes int64, maxBytesSet bool) error
	DeleteTaskByID(id string) error
	TasksInfo() (*TasksInfo, error)
	Tasks(ctx context.Context, limit int32) (chan *Task, error)
	TasksStore() (*jsm.Manager, *jsm.Stream, error)
	ElectionStorage() (nats.KeyValue, error)
}

type ScheduledTaskStorage interface {
	SaveScheduledTask(st *ScheduledTask, update bool) error
	LoadScheduledTaskByName(name string) (*ScheduledTask, error)
	DeleteScheduledTaskByName(name string) error
	ScheduledTasks(ctx context.Context) ([]*ScheduledTask, error)
	ScheduledTasksWatch(ctx context.Context) (chan *ScheduleWatchEntry, error)
	EnqueueTask(ctx context.Context, queue *Queue, task *Task) error
	ElectionStorage() (nats.KeyValue, error)
	PublishLeaderElectedEvent(ctx context.Context, name string, component string) error
}

// Storage implements the backend access
type Storage interface {
	SaveTaskState(ctx context.Context, task *Task, notify bool) error
	EnqueueTask(ctx context.Context, queue *Queue, task *Task) error
	RetryTaskByID(ctx context.Context, queue *Queue, id string) error
	LoadTaskByID(id string) (*Task, error)
	DeleteTaskByID(id string) error
	PublishTaskStateChangeEvent(ctx context.Context, task *Task) error
	AckItem(ctx context.Context, item *ProcessItem) error
	NakBlockedItem(ctx context.Context, item *ProcessItem) error
	NakItem(ctx context.Context, item *ProcessItem) error
	TerminateItem(ctx context.Context, item *ProcessItem) error
	PollQueue(ctx context.Context, q *Queue) (*ProcessItem, error)
	PrepareQueue(q *Queue, replicas int, memory bool) error
	PrepareTasks(memory bool, replicas int, retention time.Duration, maxBytes int64, maxBytesSet bool) error
	PrepareConfigurationStore(memory bool, replicas int, maxBytes int64, maxBytesSet bool) error
	SaveScheduledTask(st *ScheduledTask, update bool) error
	LoadScheduledTaskByName(name string) (*ScheduledTask, error)
	DeleteScheduledTaskByName(name string) error
	ScheduledTasks(ctx context.Context) ([]*ScheduledTask, error)
	ScheduledTasksWatch(ctx context.Context) (chan *ScheduleWatchEntry, error)
}

var (
	validNameMatcher = regexp.MustCompile(`^[a-zA-Z0-9_:-]+$`)
)

// IsValidName is a generic strict name validator for what we want people to put in name - task names etc, things that turn into subjects
func IsValidName(name string) bool {
	return validNameMatcher.MatchString(name)
}
