// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/textproto"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/nats.go"
)

const (
	// TasksStreamName is the name of the JetStream Stream storing tasks
	TasksStreamName = "CHORIA_AJ_TASKS"
	// TasksStreamSubjects is a NATS wildcard matching all tasks
	TasksStreamSubjects = "CHORIA_AJ.T.*"
	// TasksStreamSubjectPattern is the printf pattern that can be used to find an individual task by its task ID
	TasksStreamSubjectPattern = "CHORIA_AJ.T.%s"

	// WorkStreamNamePattern is the printf pattern for determining JetStream Stream names per queue
	WorkStreamNamePattern = "CHORIA_AJ_Q_%s"
	// WorkStreamSubjectPattern is the printf pattern individual items are placed in, placeholders for JobID and JobType
	WorkStreamSubjectPattern = "CHORIA_AJ.Q.%s.%s"
	// WorkStreamSubjectWildcard is a NATS filter matching all enqueued items for any task store
	WorkStreamSubjectWildcard = "CHORIA_AJ.Q.>"
	// WorkStreamNamePrefix is the prefix that, when removed, reveals the queue name
	WorkStreamNamePrefix = "CHORIA_AJ_Q_"
)

type jetStreamStorage struct {
	nc  *nats.Conn
	mgr *jsm.Manager

	tasks *taskStorage
	retry RetryPolicy

	qStreams   map[string]*jsm.Stream
	qConsumers map[string]*jsm.Consumer

	log Logger

	mu sync.Mutex
}

type taskStorage struct {
	mgr    *jsm.Manager
	stream *jsm.Stream
}

type taskMeta struct {
	seq uint64
}

func newJetStreamStorage(nc *nats.Conn, rp RetryPolicy, log Logger) (*jetStreamStorage, error) {
	if nc == nil {
		return nil, fmt.Errorf("no connection supplied")
	}

	var err error

	s := &jetStreamStorage{
		nc:         nc,
		retry:      rp,
		log:        log,
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

	task.mu.Lock()
	so := task.storageOptions
	task.mu.Unlock()

	if so == nil {
		msg.Header.Add(api.JSExpectedLastSubjSeq, "0")
	} else {
		msg.Header.Add(api.JSExpectedLastSubjSeq, fmt.Sprintf("%d", so.(*taskMeta).seq))
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

	task.mu.Lock()
	task.storageOptions = &taskMeta{seq: ack.Sequence}
	task.mu.Unlock()

	taskUpdateCounter.WithLabelValues(string(task.State)).Inc()

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

	msg := nats.NewMsg(fmt.Sprintf(WorkStreamSubjectPattern, queue.Name, task.Type))
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

	_, err = jsm.ParsePubAck(ret)
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

func (s *jetStreamStorage) TerminateItem(ctx context.Context, item *ProcessItem) error {
	if item.storageMeta == nil {
		return fmt.Errorf("invalid storage item")
	}

	return item.storageMeta.(*nats.Msg).Term(nats.Context(ctx))
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
		next = s.retry.Duration(int(md.NumDelivered))
	} else {
		next = s.retry.Duration(20)
	}

	timeout, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	s.log.Debugf("NaKing item with %v delay", next)

	resp := fmt.Sprintf(`%s {"delay": %d}`, api.AckNak, next)
	_, err = s.nc.RequestWithContext(timeout, msg.Reply, []byte(resp))
	return err
}

func (s *jetStreamStorage) PollQueue(ctx context.Context, q *Queue) (*ProcessItem, error) {
	s.mu.Lock()
	qc, ok := s.qConsumers[q.Name]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("invalid queue storage state")
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, fmt.Errorf("non deadline context given")
	}

	rj, err := json.Marshal(&api.JSApiConsumerGetNextRequest{Batch: 1, Expires: time.Until(deadline)})
	if err != nil {
		workQueuePollErrorCounter.WithLabelValues(q.Name).Inc()
		return nil, err
	}

	msg, err := s.nc.RequestWithContext(ctx, qc.NextSubject(), rj)
	if err != nil {
		if err != context.DeadlineExceeded {
			s.log.Errorf("Polling failed: %v", err)
			workQueuePollErrorCounter.WithLabelValues(q.Name).Inc()
		}
		return nil, err
	}
	status := msg.Header.Get("Status")
	if status == "404" || status == "409" || status == "408" {
		return nil, nil
	}

	if len(msg.Data) == 0 {
		s.log.Debugf("0 byte payload with headers %#v", msg.Header)
		workQueueEntryCorruptCounter.WithLabelValues(q.Name).Inc()
		msg.Term() // data is corrupt so we terminate it, no associated job to update
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

func (s *jetStreamStorage) createQueue(q *Queue, replicas int, memory bool) error {
	if q.MaxTries == 0 {
		q.MaxTries = DefaultMaxTries
	}

	if q.MaxRunTime == 0 {
		q.MaxRunTime = DefaultJobRunTime
	}

	if q.MaxConcurrent == 0 {
		q.MaxConcurrent = DefaultQueueMaxConcurrent
	}

	opts := []jsm.StreamOption{
		jsm.Subjects(fmt.Sprintf(WorkStreamSubjectPattern, q.Name, ">")),
		jsm.WorkQueueRetention(),
		jsm.Replicas(replicas),
		jsm.StreamDescription("Choria Async Jobs Work Queue"),
	}

	if memory {
		opts = append(opts, jsm.MemoryStorage())
	} else {
		opts = append(opts, jsm.FileStorage())
	}
	if q.MaxAge > 0 {
		opts = append(opts, jsm.MaxAge(q.MaxAge))
	} else {
		opts = append(opts, jsm.MaxAge(0))
	}
	if q.MaxEntries > 0 {
		opts = append(opts, jsm.MaxMessages(int64(q.MaxEntries)))
	}
	if q.DiscardOld {
		opts = append(opts, jsm.DiscardOld())
	} else {
		opts = append(opts, jsm.DiscardNew())
	}

	var err error

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

	return s.updateQueueSettings(q)
}

func (s *jetStreamStorage) updateQueueSettings(q *Queue) error {
	ss, sok := s.qStreams[q.Name]
	sc, cok := s.qConsumers[q.Name]
	if !sok || !cok {
		return fmt.Errorf("unknown queue %s", q.Name)
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	q.MaxRunTime = sc.AckWait()
	q.MaxConcurrent = sc.MaxAckPending()
	q.MaxTries = sc.MaxDeliver()
	q.DiscardOld = ss.Configuration().Discard == api.DiscardOld
	q.MaxAge = ss.MaxAge()
	q.MaxEntries = int(ss.MaxMsgs())

	return nil
}

func (s *jetStreamStorage) joinQueue(q *Queue) error {
	var err error

	s.qStreams[q.Name], err = s.mgr.LoadStream(fmt.Sprintf(WorkStreamNamePattern, q.Name))
	if err != nil {
		if jsm.IsNatsError(err, 10059) {
			return fmt.Errorf("work queue not found")
		}
		return err
	}

	s.qConsumers[q.Name], err = s.qStreams[q.Name].LoadConsumer("WORKERS")
	if err != nil {
		if jsm.IsNatsError(err, 10014) {
			return fmt.Errorf("work queue consumer not found")
		}
		return err
	}

	return s.updateQueueSettings(q)
}

func (s *jetStreamStorage) PrepareQueue(q *Queue, replicas int, memory bool) error {
	if q.Name == "" {
		return fmt.Errorf("name is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if q.NoCreate {
		return s.joinQueue(q)
	}

	return s.createQueue(q, replicas, memory)
}

func (s *jetStreamStorage) TasksInfo() (*TasksInfo, error) {
	res := &TasksInfo{
		Time: time.Now().UTC(),
	}

	var err error
	res.Stream, err = s.tasks.stream.Information()
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (s *jetStreamStorage) DeleteTaskByID(id string) error {
	msg, err := s.tasks.stream.ReadLastMessageForSubject(fmt.Sprintf(TasksStreamSubjectPattern, id))
	if err != nil {
		if jsm.IsNatsError(err, 10037) {
			return ErrTaskNotFound
		}
		return err
	}

	return s.tasks.stream.DeleteMessage(msg.Sequence)
}

func (s *jetStreamStorage) LoadTaskByID(id string) (*Task, error) {
	msg, err := s.tasks.stream.ReadLastMessageForSubject(fmt.Sprintf(TasksStreamSubjectPattern, id))
	if err != nil {
		if jsm.IsNatsError(err, 10037) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}

	task := &Task{}
	err = json.Unmarshal(msg.Data, task)
	if err != nil {
		return nil, err
	}

	task.mu.Lock()
	task.storageOptions = &taskMeta{seq: msg.Sequence}
	task.mu.Unlock()

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
		jsm.StreamDescription("Choria Async Jobs Tasks"),
	}

	if memory {
		opts = append(opts, jsm.MemoryStorage())
	} else {
		opts = append(opts, jsm.FileStorage())
	}

	opts = append(opts, jsm.MaxAge(retention))

	s.tasks = &taskStorage{mgr: s.mgr}
	s.tasks.stream, err = s.mgr.LoadOrNewStream(TasksStreamName, opts...)
	if err != nil {
		return err
	}

	return nil
}

// DeleteQueue removes a queue and all its items
func (s *jetStreamStorage) DeleteQueue(name string) error {
	stream, err := s.mgr.LoadStream(fmt.Sprintf(WorkStreamNamePattern, name))
	if err != nil {
		if jsm.IsNatsError(err, 10059) {
			return ErrQueueNotFound
		}
		return err
	}

	return stream.Delete()
}

// PurgeQueue removes all work items from the named work queue
func (s *jetStreamStorage) PurgeQueue(name string) error {
	stream, err := s.mgr.LoadStream(fmt.Sprintf(WorkStreamNamePattern, name))
	if err != nil {
		if jsm.IsNatsError(err, 10059) {
			return ErrQueueNotFound
		}
		return err
	}

	return stream.Purge()
}

// QueueInfo loads information for a named queue
func (s *jetStreamStorage) QueueInfo(name string) (*QueueInfo, error) {
	nfo := &QueueInfo{
		Name: name,
		Time: time.Now().UTC(),
	}

	stream, err := s.mgr.LoadStream(fmt.Sprintf(WorkStreamNamePattern, name))
	if err != nil {
		if jsm.IsNatsError(err, 10059) {
			return nil, ErrQueueNotFound
		}
		return nil, err
	}
	consumer, err := stream.LoadConsumer("WORKERS")
	if err != nil {
		return nil, err
	}

	nfo.Stream, err = stream.LatestInformation()
	if err != nil {
		return nil, err
	}
	cs, err := consumer.LatestState()
	if err != nil {
		return nil, err
	}
	nfo.Consumer = &cs

	return nfo, err
}

// QueueNames finds all known queues in the storage
func (s *jetStreamStorage) QueueNames() ([]string, error) {
	var result []string

	names, err := s.mgr.StreamNames(&jsm.StreamNamesFilter{Subject: WorkStreamSubjectWildcard})
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		result = append(result, strings.TrimPrefix(name, WorkStreamNamePrefix))
	}

	return result, nil
}

// Queues load full information for every found queue
func (s *jetStreamStorage) Queues() ([]*QueueInfo, error) {
	var result []*QueueInfo

	names, err := s.QueueNames()
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		nfo, err := s.QueueInfo(name)
		if err != nil {
			return nil, err
		}
		result = append(result, nfo)
	}

	return result, nil
}

func (s *jetStreamStorage) TasksStore() (*jsm.Manager, *jsm.Stream, error) {
	return s.tasks.mgr, s.tasks.stream, nil
}

func (s *jetStreamStorage) Tasks(ctx context.Context, limit int32) (chan *Task, error) {
	nfo, err := s.tasks.stream.State()
	if err != nil {
		return nil, err
	}
	if nfo.Msgs == 0 {
		return nil, fmt.Errorf("no tasks found")
	}

	out := make(chan *Task, limit)
	cnt := int32(0)

	timeout, cancel := context.WithTimeout(ctx, time.Minute)

	sub, err := s.nc.Subscribe(s.nc.NewRespInbox(), func(msg *nats.Msg) {
		if len(msg.Data) > 0 {
			task := &Task{}
			err := json.Unmarshal(msg.Data, task)
			if err != nil {
				return
			}

			select {
			case out <- task:
				md, err := msg.Metadata()
				if err != nil {
					return
				}
				done := atomic.AddInt32(&cnt, 1)
				if md.NumPending == 0 || done == limit {
					msg.Sub.Unsubscribe()
					cancel()
				}
			default:
				msg.Sub.Unsubscribe()
				cancel()
			}
		}
	})
	if err != nil {
		return nil, err
	}

	_, err = s.tasks.stream.NewConsumer(jsm.DeliverAllAvailable(), jsm.DeliverySubject(sub.Subject))
	if err != nil {
		return nil, err
	}

	go func() {
		<-timeout.Done()
		close(out)
		sub.Unsubscribe()
	}()

	return out, nil
}

const (
	hdrLine   = "NATS/1.0\r\n"
	crlf      = "\r\n"
	hdrPreEnd = len(hdrLine) - len(crlf)
)

func decodeHeadersMsg(data []byte) (http.Header, error) {
	tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(data)))
	if l, err := tp.ReadLine(); err != nil || l != hdrLine[:hdrPreEnd] {
		return nil, fmt.Errorf("could not decode headers")
	}

	mh, err := tp.ReadMIMEHeader()
	if err != nil {
		return nil, fmt.Errorf("could not decode headers")
	}

	return http.Header(mh), nil
}
