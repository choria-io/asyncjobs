// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package jsaj

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Client struct {
	opts    *ClientOpts
	storage Storage

	mu sync.Mutex
}

type Storage interface {
	SaveTaskState(ctx context.Context, task *Task) error
	EnqueueTask(ctx context.Context, queue *Queue, task *Task) error
	AckItem(ctx context.Context, item *ProcessItem) error
	NakItem(ctx context.Context, item *ProcessItem) error
	PollQueue(ctx context.Context, q *Queue) (*ProcessItem, error)
	PrepareQueue(q *Queue, replicas int, memory bool) error
	PrepareTasks(memory bool, replicas int, retention time.Duration) error
	LoadTaskByID(id string) (*Task, error)
}

func NewClient(opts ...ClientOpt) (*Client, error) {
	copts := &ClientOpts{
		replicas:    1,
		queues:      map[string]*Queue{},
		concurrency: 10,
		retryPolicy: RetryDefault,
	}

	var err error
	for _, opt := range opts {
		err = opt(copts)
		if err != nil {
			return nil, err
		}
	}

	c := &Client{opts: copts}
	c.storage, err = newJetStreamStorage(copts.nc, copts.retryPolicy)
	if err != nil {
		return nil, err
	}

	if len(c.opts.queues) == 0 {
		log.Printf("Creating %s queue with no user defined queues set", defaultQueue.Name)
		c.opts.queues[defaultQueue.Name] = &defaultQueue
	}

	err = c.setupStreams()
	if err != nil {
		return nil, err
	}

	err = c.setupQueues()
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Client) Run(ctx context.Context, router *Mux) error {
	proc, err := newProcessor(c)
	if err != nil {
		return err
	}

	c.startPrometheus()

	return proc.processMessages(ctx, router)
}

func (c *Client) startPrometheus() {
	if c.opts.statsPort == 0 {
		return
	}

	log.Printf("Exposing Prometheus metrics on port %d", c.opts.statsPort)
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(fmt.Sprintf(":%d", c.opts.statsPort), nil)
}

func (c *Client) setupStreams() error {
	return c.storage.PrepareTasks(c.opts.memoryStore, c.opts.replicas, c.opts.taskRetention)
}

func (c *Client) EnqueueTask(ctx context.Context, queue string, task *Task) error {
	q, ok := c.opts.queues[queue]
	if !ok {
		return fmt.Errorf("unknown queue: %s", queue)
	}

	return q.EnqueueTask(ctx, task)
}

func (c *Client) LoadTaskByID(id string) (*Task, error) {
	return c.storage.LoadTaskByID(id)
}

func nowPointer() *time.Time {
	t := time.Now().UTC()
	return &t
}

func (c *Client) setTaskActive(ctx context.Context, t *Task) error {
	t.State = TaskStateActive
	t.LastTriedAt = nowPointer()

	return c.storage.SaveTaskState(ctx, t)
}

func (c *Client) setTaskSuccess(ctx context.Context, t *Task, payload []byte) error {
	t.LastTriedAt = nowPointer()
	t.State = TaskStateCompleted
	t.Result = &TaskResult{
		Payload:     payload,
		CompletedAt: time.Now().UTC(),
	}

	return c.storage.SaveTaskState(ctx, t)
}

func (c *Client) handleTaskError(ctx context.Context, t *Task, err error) error {
	t.LastErr = err.Error()
	t.LastTriedAt = nowPointer()
	t.State = TaskStateRetry

	if t.Queue != "" {
		q, ok := c.opts.queues[t.Queue]
		if ok {
			if q.MaxTries == t.Tries {
				log.Printf("Expiring task %s after %d / %d tries", t.ID, t.Tries, q.MaxTries)
				t.State = TaskStateExpired
			}
		}
	}

	return c.storage.SaveTaskState(ctx, t)
}

func (c *Client) setupQueues() error {
	for _, q := range c.opts.queues {
		q.storage = c.storage

		err := c.storage.PrepareQueue(q, c.opts.replicas, c.opts.memoryStore)
		if err != nil {
			return err
		}
	}

	return nil
}
