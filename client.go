package jsaj

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	TasksStreamName           = "JSAJ_T" // stores tasks
	TasksStreamSubjects       = "JSAJ.T.*"
	TasksStreamSubjectPattern = "JSAJ.T.%s"

	WorkStreamNamePattern    = "JSAJ_Q_%s" // individual work queues
	WorkStreamSubjectPattern = "JSAJ.Q.%s"

	DefaultJobRunTime         = time.Hour
	DefaultPriority           = 5
	DefaultMaxTries           = 10
	DefaultQueueMaxConcurrent = 100
)

type Client struct {
	opts *ClientOpts

	taskS *jsm.Stream

	mgr *jsm.Manager
	nc  *nats.Conn
	mu  sync.Mutex
}

func NewClient(opts ...ClientOpt) (*Client, error) {
	copts := &ClientOpts{
		replicas:    1,
		queues:      map[string]*Queue{},
		concurrency: 10,
		retryPolicy: RetryDefault,
	}

	for _, opt := range opts {
		err := opt(copts)
		if err != nil {
			return nil, err
		}
	}

	if copts.nc == nil {
		return nil, fmt.Errorf("no NATS connection supplied")
	}

	mgr, err := jsm.New(copts.nc)
	if err != nil {
		return nil, err
	}

	c := &Client{mgr: mgr, nc: copts.nc, opts: copts}

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
	if c.opts.statsPort > 0 {
		log.Printf("Exposing Prometheus metrics on port %d", c.opts.statsPort)
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(fmt.Sprintf(":%d", c.opts.statsPort), nil)
	}

	proc, err := newProcessor(c)
	if err != nil {
		return err
	}

	return proc.processMessages(ctx, router)
}

func (c *Client) setupStreams() error {
	var err error

	opts := []jsm.StreamOption{
		jsm.Subjects(TasksStreamSubjects),
		jsm.MaxMessagesPerSubject(1),
		jsm.Replicas(c.opts.replicas),
	}

	if c.opts.memoryStore {
		opts = append(opts, jsm.MemoryStorage())
	} else {
		opts = append(opts, jsm.FileStorage())
	}

	if c.opts.taskRetention > 0 {
		opts = append(opts, jsm.MaxAge(c.opts.taskRetention))
	}

	c.taskS, err = c.mgr.LoadOrNewStream(TasksStreamName, opts...)
	if err != nil {
		return err
	}

	return err
}

func (c *Client) EnqueueTask(ctx context.Context, queue string, task *Task) error {
	q, ok := c.opts.queues[queue]
	if !ok {
		return fmt.Errorf("unknown queue: %s", queue)
	}

	task.Queue = q.Name

	jt, err := json.Marshal(task)
	if err != nil {
		return err
	}

	ji, err := newProcessItem(TaskItem, task.ID)
	if err != nil {
		return err
	}

	msg := nats.NewMsg(task.enqueueSubject)
	msg.Header.Add(api.JSExpectedLastSubjSeq, "0") // ensures no existing task
	msg.Data = jt
	ret, err := c.nc.RequestMsgWithContext(ctx, msg)
	if err != nil {
		taskUpdateErrorCounter.WithLabelValues().Inc()
		return err
	}
	_, err = jsm.ParsePubAck(ret)
	if err != nil {
		taskUpdateErrorCounter.WithLabelValues().Inc()
		return err
	}

	msg = nats.NewMsg(q.enqueueSubject)
	msg.Header.Add(api.JSMsgId, task.ID) // dedupe on the queue, though should not be needed
	msg.Data = ji
	ret, err = c.nc.RequestMsgWithContext(ctx, msg)
	if err != nil {
		enqueueErrorCounter.WithLabelValues(queue).Inc()
		task.State = TaskStateQueueError
		if err := c.saveTaskState(ctx, task); err != nil {
			return err
		}
		return err
	}

	_, err = jsm.ParsePubAck(ret) // TODO double check we actually handle whatever happens for duplicates etc
	if err != nil {
		enqueueErrorCounter.WithLabelValues(queue).Inc()
		task.State = TaskStateQueueError
		if err := c.saveTaskState(ctx, task); err != nil {
			return err
		}
		return err
	}

	enqueueCounter.WithLabelValues(queue).Inc()

	return nil
}

func (c *Client) LoadTaskByID(id string) (*Task, error) {
	msg, err := c.taskS.ReadLastMessageForSubject(fmt.Sprintf(TasksStreamSubjectPattern, id))
	if err != nil {
		return nil, err
	}

	task := &Task{}
	err = json.Unmarshal(msg.Data, task)
	if err != nil {
		return nil, err
	}

	task.seq = msg.Sequence
	task.init()

	return task, nil
}

func nowPointer() *time.Time {
	t := time.Now().UTC()
	return &t
}

func (c *Client) saveTaskState(ctx context.Context, t *Task) error {
	jt, err := json.Marshal(t)
	if err != nil {
		return err
	}

	msg := nats.NewMsg(t.enqueueSubject)
	msg.Header.Add(api.JSExpectedLastSubjSeq, fmt.Sprintf("%d", t.seq))
	msg.Data = jt

	resp, err := c.nc.RequestMsgWithContext(ctx, msg)
	if err != nil {
		taskUpdateErrorCounter.WithLabelValues().Inc()
		return err
	}

	ack, err := jsm.ParsePubAck(resp)
	if err != nil {
		taskUpdateErrorCounter.WithLabelValues().Inc()
		return err
	}

	t.seq = ack.Sequence

	taskUpdateCounter.WithLabelValues().Inc()

	return nil
}

func (c *Client) setTaskActive(ctx context.Context, t *Task) error {
	t.State = TaskStateActive
	t.LastTriedAt = nowPointer()

	return c.saveTaskState(ctx, t)
}

func (c *Client) setTaskSuccess(ctx context.Context, t *Task, payload []byte) error {
	t.LastTriedAt = nowPointer()
	t.State = TaskStateCompleted
	t.Result = &TaskResult{
		Payload:     payload,
		CompletedAt: time.Now().UTC(),
	}

	return c.saveTaskState(ctx, t)
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

	return c.saveTaskState(ctx, t)
}

func (c *Client) setupQueues() error {
	for _, q := range c.opts.queues {
		q.c = c

		err := q.setupStreams()
		if err != nil {
			return err
		}
	}

	return nil
}
