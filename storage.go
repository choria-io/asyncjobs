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
	"sync"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/nats.go"
)

const (
	TasksStreamName           = "CHORIA_AJ_TASKS" // stores tasks
	TasksStreamSubjects       = "CHORIA_AJ.T.*"
	TasksStreamSubjectPattern = "CHORIA_AJ.T.%s"

	WorkStreamNamePattern    = "CHORIA_AJ_Q_%s" // individual work queues
	WorkStreamSubjectPattern = "CHORIA_AJ.Q.%s"
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
	mgr    *jsm.Manager
	stream *jsm.Stream
}

type taskMeta struct {
	seq uint64
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

func (s *jetStreamStorage) PrepareQueue(q *Queue, replicas int, memory bool) error {
	var err error

	if q.Name == "" {
		return fmt.Errorf("name is required")
	}

	if q.Priority == 0 {
		q.Priority = DefaultPriority
	}

	if q.MaxTries == 0 {
		q.MaxTries = DefaultMaxTries
	}

	if q.MaxRunTime == 0 {
		q.MaxRunTime = DefaultJobRunTime
	}

	if q.MaxConcurrent == 0 {
		q.MaxConcurrent = DefaultQueueMaxConcurrent
	}

	if q.Priority > 10 || q.Priority < 1 {
		return fmt.Errorf("invalid priority %d on queue %s, must be between 1 and 10", q.Priority, q.Name)
	}

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

	s.mu.Lock()
	s.qStreams[q.Name], err = s.mgr.LoadOrNewStream(fmt.Sprintf(WorkStreamNamePattern, q.Name), opts...)
	s.mu.Unlock()
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
	s.mu.Lock()
	s.qConsumers[q.Name], err = s.qStreams[q.Name].LoadOrNewConsumer("WORKERS", wopts...)
	s.mu.Unlock()
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
