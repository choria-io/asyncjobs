package jsaj

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/nats.go"
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

type jetStreamStorage struct {
	nc  *nats.Conn
	mgr *jsm.Manager

	tasks *taskStorage
	retry RetryPolicy

	qStreams   map[string]*jsm.Stream
	qConsumers map[string]*jsm.Consumer

	mu sync.Mutex
}

type taskStorage struct {
	replicas      int
	memoryBacked  bool
	taskRetention time.Duration
	mgr           *jsm.Manager
	stream        *jsm.Stream
}

type taskMeta struct {
	seq uint64
	msg *nats.Msg
}

func newJetStreamStorage(nc *nats.Conn, rp RetryPolicy) (*jetStreamStorage, error) {
	if nc == nil {
		return nil, fmt.Errorf("no connection supplied")
	}

	var err error

	s := &jetStreamStorage{
		nc:         nc,
		retry:      rp,
		qStreams:   map[string]*jsm.Stream{},
		qConsumers: map[string]*jsm.Consumer{},
	}

	s.mgr, err = jsm.New(nc)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *jetStreamStorage) SaveTaskState(ctx context.Context, task *Task) error {
	jt, err := json.Marshal(task)
	if err != nil {
		return err
	}

	msg := nats.NewMsg(fmt.Sprintf(TasksStreamSubjectPattern, task.ID))
	msg.Data = jt
	if task.storageOptions == nil {
		msg.Header.Add(api.JSExpectedLastSubjSeq, "0")
	} else {
		msg.Header.Add(api.JSExpectedLastSubjSeq, fmt.Sprintf("%d", task.storageOptions.(*taskMeta).seq))
	}

	resp, err := s.nc.RequestMsgWithContext(ctx, msg)
	if err != nil {
		taskUpdateErrorCounter.WithLabelValues().Inc()
		return err
	}

	ack, err := jsm.ParsePubAck(resp)
	if err != nil {
		taskUpdateErrorCounter.WithLabelValues().Inc()
		return err
	}

	task.storageOptions = &taskMeta{seq: ack.Sequence}

	taskUpdateCounter.WithLabelValues().Inc()

	return nil

}

func (s *jetStreamStorage) EnqueueTask(ctx context.Context, queue *Queue, task *Task) error {
	ji, err := newProcessItem(TaskItem, task.ID)
	if err != nil {
		return err
	}

	err = s.SaveTaskState(ctx, task)
	if err != nil {
		return err
	}

	msg := nats.NewMsg(fmt.Sprintf(WorkStreamSubjectPattern, queue.Name))
	msg.Header.Add(api.JSMsgId, task.ID) // dedupe on the queue, though should not be needed
	msg.Data = ji
	ret, err := s.nc.RequestMsgWithContext(ctx, msg)
	if err != nil {
		enqueueErrorCounter.WithLabelValues(queue.Name).Inc()
		task.State = TaskStateQueueError
		if err := s.SaveTaskState(ctx, task); err != nil {
			return err
		}
		return err
	}

	_, err = jsm.ParsePubAck(ret) // TODO double check we actually handle whatever happens for duplicates etc
	if err != nil {
		enqueueErrorCounter.WithLabelValues(queue.Name).Inc()
		task.State = TaskStateQueueError
		if err := s.SaveTaskState(ctx, task); err != nil {
			return err
		}
		return err
	}

	enqueueCounter.WithLabelValues(queue.Name).Inc()

	return nil
}

func (s *jetStreamStorage) AckItem(ctx context.Context, item *ProcessItem) error {
	if item.storageMeta == nil {
		return fmt.Errorf("invalid storage item")
	}

	return item.storageMeta.(*nats.Msg).Ack(nats.Context(ctx))
}

func (s *jetStreamStorage) NakItem(ctx context.Context, item *ProcessItem) error {
	if item.storageMeta == nil {
		return fmt.Errorf("invalid storage item")
	}

	msg := item.storageMeta.(*nats.Msg)

	var next time.Duration

	// for when getting metadata fails, this ensures a delay with jitter
	md, err := msg.Metadata()
	if err == nil {
		next = s.retry.Duration(int(md.NumDelivered) - 1)
	} else {
		next = s.retry.Duration(20)
	}

	log.Printf("Delaying reprocessing by %v", next)
	resp := fmt.Sprintf(`%s {"delay": %d}`, api.AckNak, next)
	return msg.Respond([]byte(resp))
}

func (s *jetStreamStorage) PollQueue(ctx context.Context, q *Queue) (*ProcessItem, error) {
	qc, ok := s.qConsumers[q.Name]
	if !ok {
		return nil, fmt.Errorf("invalid queue storage state")
	}

	rj, err := json.Marshal(&api.JSApiConsumerGetNextRequest{Batch: 1, NoWait: true})
	if err != nil {
		workQueuePollErrorCounter.WithLabelValues(q.Name).Inc()
		return nil, err
	}

	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	msg, err := s.nc.RequestWithContext(rctx, qc.NextSubject(), rj)
	if err != nil {
		workQueuePollErrorCounter.WithLabelValues(q.Name).Inc()
		return nil, err
	}

	status := msg.Header.Get("Status")
	if status == "404" || status == "409" {
		return nil, nil
	}

	if len(msg.Data) == 0 {
		workQueueEntryCorruptCounter.WithLabelValues(q.Name).Inc()
		return nil, fmt.Errorf("invalid queue item received")
	}

	item := &ProcessItem{storageMeta: msg}
	err = json.Unmarshal(msg.Data, item)
	if err != nil || item.JobID == "" {
		workQueueEntryCorruptCounter.WithLabelValues(q.Name).Inc()
		msg.Term() // data is corrupt so we terminate it, no associated job to update
		return nil, fmt.Errorf("corrupt queue item received")
	}

	return item, nil
}

func (s *jetStreamStorage) PrepareQueue(q *Queue, replicas int, memory bool) error {
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

	// q.enqueueSubject = fmt.Sprintf(WorkStreamSubjectPattern, q.Name)

	opts := []jsm.StreamOption{
		jsm.Subjects(fmt.Sprintf(WorkStreamSubjectPattern, q.Name)),
		jsm.WorkQueueRetention(),
		jsm.Replicas(replicas),
	}

	if memory {
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

	s.qStreams[q.Name], err = s.mgr.LoadOrNewStream(fmt.Sprintf(WorkStreamNamePattern, q.Name), opts...)
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
	s.qConsumers[q.Name], err = s.qStreams[q.Name].LoadOrNewConsumer("WORKERS", wopts...)
	if err != nil {
		return err
	}

	return nil

}

func (s *jetStreamStorage) LoadTaskByID(id string) (*Task, error) {
	msg, err := s.tasks.stream.ReadLastMessageForSubject(fmt.Sprintf(TasksStreamSubjectPattern, id))
	if err != nil {
		return nil, err
	}

	task := &Task{}
	err = json.Unmarshal(msg.Data, task)
	if err != nil {
		return nil, err
	}

	task.storageOptions = &taskMeta{seq: msg.Sequence}

	return task, nil
}

func (s *jetStreamStorage) PrepareTasks(memory bool, replicas int, retention time.Duration) error {
	var err error

	if replicas == 0 {
		replicas = 1
	}

	opts := []jsm.StreamOption{
		jsm.Subjects(TasksStreamSubjects),
		jsm.MaxMessagesPerSubject(1),
		jsm.Replicas(replicas),
	}

	if memory {
		opts = append(opts, jsm.MemoryStorage())
	} else {
		opts = append(opts, jsm.FileStorage())
	}

	if retention > 0 {
		opts = append(opts, jsm.MaxAge(retention))
	}

	s.tasks = &taskStorage{mgr: s.mgr}
	s.tasks.stream, err = s.mgr.LoadOrNewStream(TasksStreamName, opts...)
	if err != nil {
		return err
	}

	return nil
}
