package jsaj

import (
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/jsm.go"
)

type Queue struct {
	Name       string
	MaxAge     time.Duration
	MaxEntries int
	DiscardOld bool // when MaxEntries is reached
	Priority   int
	MaxTries   int
	MaxRunTime time.Duration

	taskS          *jsm.Stream
	taskC          *jsm.Consumer
	nextSubj       string
	enqueueSubject string

	c  *Client
	mu sync.Mutex
}

var defaultQueue = Queue{
	Name:       "DEFAULT",
	MaxRunTime: 5 * time.Second,
	MaxTries:   2,
}

func (q *Queue) setupStreams() error {
	var err error

	opts := []jsm.StreamOption{
		jsm.Subjects(fmt.Sprintf(WorkStreamSubjectPattern, q.Name)),
		jsm.WorkQueueRetention(),
		jsm.FileStorage(),
		jsm.Replicas(q.c.opts.replicas),
	}

	if q.MaxAge > 0 {
		opts = append(opts, jsm.MaxAge(q.MaxAge))
	}
	if q.MaxEntries > 0 {
		opts = append(opts, jsm.MaxMessages(int64(q.MaxEntries)))
	}
	if q.DiscardOld {
		opts = append(opts, jsm.DiscardOld())
	}

	q.taskS, err = q.c.mgr.LoadOrNewStream(fmt.Sprintf(WorkStreamNamePattern, q.Name), opts...)
	if err != nil {
		return err
	}

	wopts := []jsm.ConsumerOption{
		jsm.DurableName("WORKERS"),
		jsm.AckWait(q.MaxRunTime),
		jsm.MaxAckPending(uint(q.c.opts.concurrency)),
		jsm.AcknowledgeExplicit(),
		jsm.MaxDeliveryAttempts(q.MaxTries),
	}

	q.taskC, err = q.taskS.LoadOrNewConsumer("WORKERS", wopts...)
	if err != nil {
		return err
	}
	q.nextSubj = q.taskC.NextSubject()

	return nil
}
