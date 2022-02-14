// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type processor struct {
	queue       *Queue
	mux         *Mux
	c           *Client
	concurrency int
	limiter     chan struct{}
	retryPolicy RetryPolicyProvider
	log         Logger

	mu *sync.Mutex
}

// ItemKind indicates the kind of job a work queue entry represents
type ItemKind int

var (
	// TaskItem is a task as defined by Task
	TaskItem ItemKind = 0
)

// ProcessItem is an individual item stored in the work queue
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
		queue:       c.opts.queue,
		concurrency: c.opts.concurrency,
		limiter:     make(chan struct{}, c.opts.concurrency),
		retryPolicy: c.opts.retryPolicy,
		log:         c.log,
		mu:          &sync.Mutex{},
	}

	for i := 0; i < cap(p.limiter); i++ {
		p.limiter <- struct{}{}
	}

	return p, nil
}

func (p *processor) processMessage(ctx context.Context, item *ProcessItem) error {
	task, err := p.c.LoadTaskByID(item.JobID)
	if err != nil {
		workQueueEntryForUnknownTaskErrorCounter.WithLabelValues(p.queue.Name).Inc()
		if errors.Is(err, ErrTaskNotFound) {
			p.log.Warnf("Could not find task data for %s, discarding work item", item.JobID)
			p.c.storage.TerminateItem(ctx, item)
			return nil
		}

		return fmt.Errorf("%s: %s", ErrTaskLoadFailed, err)
	}

	switch task.State {
	case TaskStateActive:
		if task.LastTriedAt == nil || time.Since(*task.LastTriedAt) < p.queue.MaxRunTime {
			return ErrTaskAlreadyActive
		}

	case TaskStateCompleted, TaskStateExpired:
		p.c.storage.AckItem(ctx, item)
		return fmt.Errorf("%w %q", ErrTaskAlreadyInState, task.State)
	}

	if task.IsPastDeadline() {
		workQueueEntryPastDeadlineCounter.WithLabelValues(p.queue.Name).Inc()
		err = p.c.handleTaskExpired(ctx, task)
		if err != nil {
			p.log.Warnf("Could not expire task %s: %v", task.ID, err)
		}
		return ErrTaskPastDeadline
	}

	if task.MaxTries > 0 && task.Tries > task.MaxTries {
		workQueueEntryPastMaxTriesCounter.WithLabelValues(p.queue.Name).Inc()
		err = p.c.handleTaskExpired(ctx, task)
		if err != nil {
			p.log.Warnf("Could not expire task %s: %v", task.ID, err)
		}
		return ErrTaskExceedsMaxTries
	}

	err = p.c.setTaskActive(ctx, task)
	if err != nil {
		return fmt.Errorf("%w %s: %v", ErrTaskUpdateFailed, task.State, err)
	}

	go p.handle(ctx, task, item, p.queue.MaxRunTime)

	return nil
}

func (p *processor) pollItem(ctx context.Context) (*ProcessItem, error) {
	ctr := 0
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		workQueuePollCounter.WithLabelValues(p.queue.Name).Inc()
		timeout, cancel := context.WithTimeout(ctx, time.Minute)
		item, err := p.c.storage.PollQueue(timeout, p.queue)
		cancel()

		switch {
		case err == context.Canceled:
			p.log.Debugf("Context canceled, terminating polling")
			return nil, err
		case err == context.DeadlineExceeded:
			p.log.Debugf("Context timeout, retrying poll")
			ctr = 0
			continue

		case err != nil:
			p.log.Debugf("Unexpected polling error: %v", err)
			workQueuePollErrorCounter.WithLabelValues(p.queue.Name).Inc()
			if RetrySleep(ctx, retryLinearTenSeconds, ctr) == context.Canceled {
				return nil, ctx.Err()
			}
			ctr++
			continue

		case item == nil:
			p.log.Debugf("Had a nil item, retrying")
			// 404 etc
			continue
		}

		return item, nil
	}
}

func (p *processor) processMessages(ctx context.Context, mux *Mux) error {
	if mux == nil {
		return ErrNoMux
	}

	p.mux = mux

	for {
		select {
		case <-p.limiter:
			item, err := p.pollItem(ctx)
			if err != nil {
				if err == context.DeadlineExceeded {
					p.log.Infof("Processor exiting on context %s", err)
					return nil
				}
				if err == context.Canceled {
					return nil
				}

				p.log.Errorf("Unexpected polling error: %v", err)
				// pollItem already logged and slept
				p.limiter <- struct{}{}
			}

			if item == nil {
				continue
			}

			p.log.Debugf("Received an Item with ID %s", item.JobID)

			err = p.processMessage(ctx, item)
			if err != nil {
				p.log.Warnf("Processing job %s failed: %v", item.JobID, err)
				p.limiter <- struct{}{}
				continue
			}
		case <-ctx.Done():
			p.log.Infof("Processor exiting on context %s", ctx.Err())
			return nil
		}
	}
}

func (p *processor) handle(ctx context.Context, t *Task, item *ProcessItem, to time.Duration) {
	defer func() {
		handlersBusyGauge.WithLabelValues().Dec()
		p.limiter <- struct{}{}
	}()

	if p.mux == nil {
		return
	}

	obs := prometheus.NewTimer(handlerRunTimeSummary.WithLabelValues(t.Queue, t.Type))
	defer obs.ObserveDuration()
	handlersBusyGauge.WithLabelValues().Inc()

	timeout, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	t.Tries++

	payload, err := p.mux.Handler(t)(timeout, p.log, t)
	if err != nil {
		if errors.Is(err, ErrTerminateTask) {
			handlersErroredCounter.WithLabelValues(t.Queue, t.Type).Inc()
			p.log.Errorf("Handling task %s failed, terminating retries: %s", t.ID, err)

			err = p.c.handleTaskTerminated(ctx, t, err)
			if err != nil {
				p.log.Warnf("Updating task after failed processing failed: %v", err)
			}

			err = p.c.storage.TerminateItem(ctx, item)
			if err != nil {
				p.log.Warnf("Term after failed processing failed: %v", err)
			}
		} else {
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
