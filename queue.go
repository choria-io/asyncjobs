package jsaj

import (
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/jsm.go"
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

	taskS          *jsm.Stream
	taskC          *jsm.Consumer
	nextSubj       string
	enqueueSubject string

	c  *Client
	mu sync.Mutex
}

var defaultQueue = Queue{
	Name:          "DEFAULT",
	MaxRunTime:    time.Minute,
	MaxTries:      100,
	MaxConcurrent: DefaultQueueMaxConcurrent,
}

func (q *Queue) setupStreams() error {
	var err error

	if q.Priority == 0 {
		q.Priority = DefaultPriority
	}

	if q.Priority > 10 || q.Priority < 1 {
		return fmt.Errorf("invalid priority %d on queue %s, must be between 1 and 10", q.Priority, q.Name)
	}
	if q.MaxTries == 0 {
		q.MaxTries = DefaultMaxTries
	}

	if q.MaxRunTime == 0 {
		q.MaxRunTime = DefaultJobRunTime
	}

	q.enqueueSubject = fmt.Sprintf(WorkStreamSubjectPattern, q.Name)

	opts := []jsm.StreamOption{
		jsm.Subjects(fmt.Sprintf(WorkStreamSubjectPattern, q.Name)),
		jsm.WorkQueueRetention(),

		jsm.Replicas(q.c.opts.replicas),
	}

	if q.c.opts.memoryStore {
		opts = append(opts, jsm.MemoryStorage())
	} else {
		opts = append(opts, jsm.FileStorage())
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
	if q.MaxConcurrent == 0 {
		q.MaxConcurrent = DefaultQueueMaxConcurrent
	}

	q.taskS, err = q.c.mgr.LoadOrNewStream(fmt.Sprintf(WorkStreamNamePattern, q.Name), opts...)
	if err != nil {
		return err
	}

	wopts := []jsm.ConsumerOption{
		jsm.DurableName("WORKERS"),
		jsm.AckWait(q.MaxRunTime),
		jsm.MaxAckPending(uint(q.MaxConcurrent)),
		jsm.AcknowledgeExplicit(),
		jsm.MaxDeliveryAttempts(q.MaxTries),
	}

	q.taskC, err = q.taskS.LoadOrNewConsumer("WORKERS", wopts...)
	if err != nil {
		return err
	}
	q.nextSubj = q.taskC.NextSubject()

	// TODO: perhaps consolidate settings from stream back into queue so that
	// binding to existing queues with wrong (or no) properties will function right

	return nil
}
