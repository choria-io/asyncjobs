// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"encoding/json"
	"math/rand"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type processor struct {
	queues      []*Queue
	mux         *Mux
	c           *Client
	limiter     chan struct{}
	retryPolicy RetryPolicy
	log         Logger
	mu          sync.Mutex
}

type ItemKind int

var (
	TaskItem ItemKind = 0
)

type ProcessItem struct {
	Kind  ItemKind `json:"kind"`
	JobID string   `json:"job"`

	storageMeta interface{}
}

func newProcessItem(kind ItemKind, id string) ([]byte, error) {
	return json.Marshal(&ProcessItem{Kind: kind, JobID: id})
}

func newProcessor(c *Client) (*processor, error) {
	p := &processor{
		c:           c,
		limiter:     make(chan struct{}, c.opts.concurrency),
		retryPolicy: c.opts.retryPolicy,
		log:         c.log,
	}

	// add it priority times to the list so a p1 would be processed once per cycle while a p10 10 times a cycle
	for _, q := range c.opts.queues {
		for i := 1; i <= q.Priority; i++ {
			p.queues = append(p.queues, q)
		}
	}

	// now shuffle them to avoid starvation
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(p.queues), func(i, j int) {
		p.queues[i], p.queues[j] = p.queues[j], p.queues[i]
	})

	for i := 0; i < cap(p.limiter); i++ {
		p.limiter <- struct{}{}
	}

	return p, nil
}

func (p *processor) processMessage(ctx context.Context, q *Queue, item *ProcessItem) {
	if item == nil {
		p.limiter <- struct{}{}
		return
	}

	task, err := p.c.LoadTaskByID(item.JobID)
	if err != nil {
		workQueueEntryForUnknownTaskErrorCounter.WithLabelValues(q.Name).Inc()
		p.log.Errorf("Loading task %q failed: %s", item.JobID, err)
		p.limiter <- struct{}{}
		return
	}

	switch task.State {
	case TaskStateActive:
		// TODO: detect stale state
		p.log.Warnf("Task %s is already in active state", task.ID)
		p.limiter <- struct{}{}
		return

	case TaskStateCompleted, TaskStateExpired:
		p.log.Warnf("Task %s is already %q", task.ID, task.State)
		p.c.storage.AckItem(ctx, item)
		p.limiter <- struct{}{}
		return
	}

	if task.Deadline != nil && time.Since(*task.Deadline) < 0 {
		workQueueEntryPastDeadlineCounter.WithLabelValues(q.Name).Inc()
		p.log.Warnf("Task %s is past its deadline of %v", task.ID, task.Deadline)
		task.State = TaskStateExpired
		err = p.c.storage.SaveTaskState(ctx, task)
		if err != nil {
			p.log.Errorf("Could not set task %s to expired state: %v", task.ID, err)
		}
		p.limiter <- struct{}{}
		return
	}

	err = p.c.setTaskActive(ctx, task)
	if err != nil {
		p.log.Errorf("Setting task active failed: %v", err)
		p.limiter <- struct{}{}
		return
	}

	go p.handle(ctx, task, item, q.MaxRunTime)
}

func (p *processor) processMessages(ctx context.Context, mux *Mux) error {
	p.mux = mux

	for {
		for _, q := range p.queues {
			select {
			case <-p.limiter:
			case <-ctx.Done():
				return ctx.Err()
			}

			workQueuePollCounter.WithLabelValues(q.Name).Inc()

			item, err := p.c.storage.PollQueue(ctx, q)
			if err != nil {
				p.log.Errorf("Polling queue %s failed: %v", q.Name, err)
			}

			p.processMessage(ctx, q, item)

			// TODO: this is grim, should be more intelligent here, either backoff when idle and speed up when there are messages
			// or build something that does longer polls on a per queue basis but with sleeps based on the priority between polls
			// to create priority perhaps, this way long polls could be used. But that would end up with unprocessed messages, at
			// least this way we poll only when we know a handler is free based on concurrency.  Perhaps short long polls.
			//
			// An alternative is to not allow multiple queues but just one queue and we pull that on a queue-per-client basis, but
			// I really like the mux idea and multiple workers here
			//
			// this now polls every 50ms when not limited by the p.limiter, bad. Asynq sleeps a second between polls so maybe I am
			// overthinking the situation
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func (p *processor) handle(ctx context.Context, t *Task, item *ProcessItem, to time.Duration) {
	defer func() {
		handlersBusyGauge.WithLabelValues().Dec()
		p.limiter <- struct{}{}
	}()

	obs := prometheus.NewTimer(handlerRunTimeSummary.WithLabelValues(t.Queue, t.Type))
	defer obs.ObserveDuration()
	handlersBusyGauge.WithLabelValues().Inc()

	timeout, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	t.Tries++

	payload, err := p.mux.Handler(t)(timeout, t)
	if err != nil {
		handlersErroredCounter.WithLabelValues(t.Queue, t.Type).Inc()
		p.log.Errorf("Handling task %s failed: %s", t.ID, err)
		err = p.c.handleTaskError(ctx, t, err)
		if err != nil {
			p.log.Warnf("Updating task after failed processing failed: %v", err)
		}

		err = p.c.storage.NakItem(ctx, item)
		if err != nil {
			p.log.Warnf("NaK after failed processing failed: %v", err)
		}

		return
	}

	err = p.c.setTaskSuccess(ctx, t, payload)
	if err != nil {
		p.log.Warnf("Updating task after processing failed: %v", err)
	}

	// we try ack the thing anyway, hail mary to avoid a retry even if setTaskSuccess failed
	err = p.c.storage.AckItem(ctx, item)
	if err != nil {
		p.log.Errorf("Acknowledging work item failed: %v", err)
	}
}
