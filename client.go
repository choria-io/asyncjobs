// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// DefaultJobRunTime when not configured for a queue this is the default run-time handlers will get
	DefaultJobRunTime = time.Hour
	// DefaultMaxTries when not configured for a queue this is the default tries it will get
	DefaultMaxTries = 10
	// DefaultQueueMaxConcurrent when not configured for a queue this is the default concurrency setting
	DefaultQueueMaxConcurrent = 100
)

// Client connects Task producers and Task handlers to the backend
type Client struct {
	opts    *ClientOpts
	storage *jetStreamStorage

	log Logger
}

var (
	// ErrTaskNotFound is the error indicating a task does not exist rather than a failure to load
	ErrTaskNotFound = errors.New("task not found")
	// ErrQueueNotFound is the error indicating a queue does not exist rather than a failure to load
	ErrQueueNotFound = errors.New("queue not found")
	// ErrTerminateTask indicates that a task failed, and no further processing attempts should be made
	ErrTerminateTask = fmt.Errorf("terminate task")
)

// Storage implements the backend access
type Storage interface {
	SaveTaskState(ctx context.Context, task *Task) error
	EnqueueTask(ctx context.Context, queue *Queue, task *Task) error
	AckItem(ctx context.Context, item *ProcessItem) error
	NakItem(ctx context.Context, item *ProcessItem) error
	TerminateItem(ctx context.Context, item *ProcessItem) error
	PollQueue(ctx context.Context, q *Queue) (*ProcessItem, error)
	PrepareQueue(q *Queue, replicas int, memory bool) error
	PrepareTasks(memory bool, replicas int, retention time.Duration) error
	LoadTaskByID(id string) (*Task, error)
}

// StorageAdmin is helpers to support the CLI mainly, this leaks a bunch of details about JetStream
// but that's ok, we're not really intending to change the storage or support more
type StorageAdmin interface {
	Queues() ([]*QueueInfo, error)
	QueueNames() ([]string, error)
	QueueInfo(name string) (*QueueInfo, error)
	PurgeQueue(name string) error
	DeleteQueue(name string) error
	PrepareQueue(q *Queue, replicas int, memory bool) error
	PrepareTasks(memory bool, replicas int, retention time.Duration) error
	TasksInfo() (*TasksInfo, error)
	LoadTaskByID(id string) (*Task, error)
	DeleteTaskByID(id string) error
	Tasks(ctx context.Context, limit int32) (chan *Task, error)
	TasksStore() (*jsm.Manager, *jsm.Stream, error)
}

// NewClient creates a new client, one of NatsConn() or NatsContext() must be passed, other options are optional.
//
// When no Queue() is supplied a default queue called DEFAULT will be used
func NewClient(opts ...ClientOpt) (*Client, error) {
	copts := &ClientOpts{
		replicas:    1,
		concurrency: 10,
		retryPolicy: RetryDefault,
		logger:      &defaultLogger{},
	}

	var err error
	for _, opt := range opts {
		err = opt(copts)
		if err != nil {
			return nil, err
		}
	}

	c := &Client{opts: copts, log: copts.logger}
	c.storage, err = newJetStreamStorage(copts.nc, copts.retryPolicy, c.log)
	if err != nil {
		return nil, err
	}

	if c.opts.queue == nil {
		c.opts.queue = newDefaultQueue()
		c.log.Debugf("Creating %s queue with no user defined queues set", c.opts.queue.Name)
	}

	if !copts.skipPrepare {
		err = c.setupStreams()
		if err != nil {
			return nil, err
		}

		err = c.setupQueues()
		if err != nil {
			return nil, err
		}
	}

	return c, nil
}

// Run starts processing messages using the router until error or interruption
func (c *Client) Run(ctx context.Context, router *Mux) error {
	if c.opts.queue == nil {
		return fmt.Errorf("no queue defined")
	}

	proc, err := newProcessor(c)
	if err != nil {
		return err
	}

	c.startPrometheus()

	return proc.processMessages(ctx, router)
}

// LoadTaskByID loads a task from the backend using its ID
func (c *Client) LoadTaskByID(id string) (*Task, error) {
	return c.storage.LoadTaskByID(id)
}

// EnqueueTask adds a task to the named queue which must already exist
func (c *Client) EnqueueTask(ctx context.Context, task *Task) error {
	return c.opts.queue.enqueueTask(ctx, task)
}

// StorageAdmin access admin features of the storage backend
func (c *Client) StorageAdmin() StorageAdmin {
	return c.storage
}

func (c *Client) startPrometheus() {
	if c.opts.statsPort == 0 {
		return
	}

	c.log.Warnf("Exposing Prometheus metrics on port %d", c.opts.statsPort)
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(fmt.Sprintf(":%d", c.opts.statsPort), nil)
}

func (c *Client) setupStreams() error {
	return c.storage.PrepareTasks(c.opts.memoryStore, c.opts.replicas, c.opts.taskRetention)
}

func nowPointer() *time.Time {
	t := time.Now().UTC()
	return &t
}

func (c *Client) setTaskActive(ctx context.Context, t *Task) error {
	t.State = TaskStateActive
	t.LastTriedAt = nowPointer()
	t.LastErr = ""

	return c.storage.SaveTaskState(ctx, t)
}

func (c *Client) shouldDiscardTask(t *Task) bool {
	for _, state := range c.opts.discard {
		if t.State == state {
			return true
		}
	}

	return false
}

func (c *Client) discardTaskIfDesired(t *Task) error {
	if !c.shouldDiscardTask(t) {
		return nil
	}

	c.log.Debugf("Discarding task with state %s based on desired discards %q", t.State, c.opts.discard)
	return c.storage.DeleteTaskByID(t.ID)
}

func (c *Client) setTaskSuccess(ctx context.Context, t *Task, payload interface{}) error {
	t.LastTriedAt = nowPointer()
	t.State = TaskStateCompleted
	t.LastErr = ""

	t.Result = &TaskResult{
		Payload:     payload,
		CompletedAt: time.Now().UTC(),
	}

	err := c.storage.SaveTaskState(ctx, t)
	if err != nil {
		return err
	}

	return c.discardTaskIfDesired(t)
}

func (c *Client) handleTaskTerminated(ctx context.Context, t *Task, terr error) error {
	t.LastErr = terr.Error()
	t.LastTriedAt = nowPointer()
	t.State = TaskStateTerminated

	err := c.storage.SaveTaskState(ctx, t)
	if err != nil {
		return err
	}

	return c.discardTaskIfDesired(t)
}

func (c *Client) handleTaskError(ctx context.Context, t *Task, terr error) error {
	t.LastErr = terr.Error()
	t.LastTriedAt = nowPointer()
	t.State = TaskStateRetry

	if t.Queue != "" && t.Queue == c.opts.queue.Name {
		if c.opts.queue.MaxTries == t.Tries {
			c.log.Infof("Expiring task %s after %d / %d tries", t.ID, t.Tries, c.opts.queue.MaxTries)
			t.State = TaskStateExpired
		}
	}

	err := c.storage.SaveTaskState(ctx, t)
	if err != nil {
		return err
	}

	return c.discardTaskIfDesired(t)
}

func (c *Client) setupQueues() error {
	c.opts.queue.storage = c.storage
	return c.storage.PrepareQueue(c.opts.queue, c.opts.replicas, c.opts.memoryStore)
}
