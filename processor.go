package jsaj

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/nats.go"
)

type processor struct {
	queues      []*Queue
	mux         *Mux
	c           *Client
	limiter     chan struct{}
	retryPolicy RetryPolicy
	mu          sync.Mutex
}

type ItemKind int

var (
	TaskItem ItemKind = 0
)

type ProcessItem struct {
	Kind  ItemKind `json:"kind"`
	JobID string   `json:"job"`
}

func newProcessItem(kind ItemKind, id string) ([]byte, error) {
	return json.Marshal(&ProcessItem{Kind: kind, JobID: id})
}

func newProcessor(c *Client) (*processor, error) {
	p := &processor{
		c:           c,
		limiter:     make(chan struct{}, c.opts.concurrency),
		retryPolicy: c.opts.retryPolicy,
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

func (p *processor) processMessage(ctx context.Context, q *Queue, msg *nats.Msg) {
	status := msg.Header.Get("Status")
	if status == "404" || status == "409" {
		return
	}

	if len(msg.Data) == 0 {
		log.Printf("Invalid message: %#v", msg)
		p.limiter <- struct{}{}
		return
	}

	item := &ProcessItem{}
	err := json.Unmarshal(msg.Data, item)
	if err != nil || item.JobID == "" {
		log.Printf("Invalid processor item: %q", msg.Data)
		msg.Term() // data is corrupt so we terminate it, no associated job to update
		p.limiter <- struct{}{}
		return
	}

	task, err := p.c.LoadTaskByID(item.JobID)
	if err != nil {
		log.Printf("Loading task %q failed: %s", msg.Data, err)
		p.limiter <- struct{}{}
		return
	}

	switch task.State {
	case TaskStateActive:
		// TODO: detect stale state
		log.Printf("Task %s is already in active state", task.ID)
		p.limiter <- struct{}{}
		return

	case TaskStateCompleted, TaskStateExpired:
		log.Printf("Task %s is already %q", task.ID, task.State)
		msg.Ack() // best efforts ok
		p.limiter <- struct{}{}
		return
	}

	if task.Deadline != nil && time.Since(*task.Deadline) < 0 {
		log.Printf("Task %s is past its deadline of %v", task.ID, task.Deadline)
		task.State = TaskStateExpired
		err = p.c.saveTaskState(ctx, task)
		if err != nil {
			log.Printf("Could not set task %s to expired state: %v", task.ID, err)
		}
		p.limiter <- struct{}{}
		return
	}

	err = p.c.setTaskActive(ctx, task)
	if err != nil {
		log.Printf("Setting task active failed: %v", err)
		p.limiter <- struct{}{}
		return
	}

	go p.handle(ctx, task, msg, q.taskC.AckWait())
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

			rj, err := json.Marshal(&api.JSApiConsumerGetNextRequest{Batch: 1, NoWait: true})
			if err != nil {
				// TODO: log
				time.Sleep(50 * time.Millisecond)
				continue
			}

			rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := p.c.nc.RequestWithContext(rctx, q.nextSubj, rj)
			cancel()
			if err != nil {
				p.limiter <- struct{}{}
				log.Printf("next msg failed: %v", err)
				time.Sleep(50 * time.Millisecond)
				continue
			}

			if resp != nil {
				p.processMessage(ctx, q, resp)
			}

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

func (p *processor) nakMsg(msg *nats.Msg) error {
	var next time.Duration
	// for when getting metadata fails, this ensures a delay with jitter
	md, err := msg.Metadata()
	if err == nil {
		next = p.retryPolicy.Duration(int(md.NumDelivered) - 1)
	} else {
		next = p.retryPolicy.Duration(20)
	}

	log.Printf("Delaying reprocessing by %v", next)
	resp := fmt.Sprintf(`%s {"delay": %d}`, api.AckNak, next)
	return msg.Respond([]byte(resp))
}

func (p *processor) handle(ctx context.Context, t *Task, msg *nats.Msg, to time.Duration) {
	defer func() {
		p.limiter <- struct{}{}
	}()

	timeout, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	t.Tries++

	payload, err := p.mux.Handler(t)(timeout, t)
	if err != nil {
		log.Printf("Handling task %s failed: %s", t.ID, err)
		err = p.c.handleTaskError(ctx, t, err)
		if err != nil {
			log.Printf("Updating task after failed processing failed: %v", err)
		}

		err = p.nakMsg(msg)
		if err != nil {
			log.Printf("NaK after failed processing failed: %v", err)
		}

		return
	}

	err = msg.Ack()
	if err != nil {
		err = p.c.handleTaskError(ctx, t, err)
		if err != nil {
			log.Printf("Acknowledging work item failed processing failed: %v", err)
			return
		}
	}

	err = p.c.setTaskSuccess(ctx, t, payload)
	if err != nil {
		log.Printf("Updating task after processing failed: %v", err)
		return
	}
}
