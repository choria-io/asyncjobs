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

	// EventsSubjectWildcard is the NATS wildcard for receiving all events
	EventsSubjectWildcard = "CHORIA_AJ.E.>"
	// TaskStateChangeEventSubjectPattern is a printf pattern for determining the event publish subject
	TaskStateChangeEventSubjectPattern = "CHORIA_AJ.E.task_state.%s"
	// TaskStateChangeEventSubjectWildcard is a NATS wildcard for receiving all TaskStateChangeEvent messages
	TaskStateChangeEventSubjectWildcard = "CHORIA_AJ.E.task_state.*"
	// LeaderElectedEventSubjectPattern is the pattern for determining the event publish subject
	LeaderElectedEventSubjectPattern = "CHORIA_AJ.E.leader_election.%s"
	// LeaderElectedEventSubjectWildcard is the NATS wildcard for receiving all LeaderElectedEvent messages
	LeaderElectedEventSubjectWildcard = "CHORIA_AJ.E.leader_election.>"

	// WorkStreamNamePattern is the printf pattern for determining JetStream Stream names per queue
	WorkStreamNamePattern = "CHORIA_AJ_Q_%s"
	// WorkStreamSubjectPattern is the printf pattern individual items are placed in, placeholders for JobID and JobType
	WorkStreamSubjectPattern = "CHORIA_AJ.Q.%s.%s"
	// WorkStreamSubjectWildcard is a NATS filter matching all enqueued items for any task store
	WorkStreamSubjectWildcard = "CHORIA_AJ.Q.>"
	// WorkStreamNamePrefix is the prefix that, when removed, reveals the queue name
	WorkStreamNamePrefix = "CHORIA_AJ_Q_"

	// RequestReplyTaskHandlerPattern is the subject request reply task handlers should listen on by default
	RequestReplyTaskHandlerPattern = "CHORIA_AJ.H.T.%s"

	// ConfigBucketName is the KV bucket for configuration like scheduled tasks
	ConfigBucketName = "CHORIA_AJ_CONFIGURATION"

	// LeaderElectionBucketName is the KV bucket that will manage leader elections
	LeaderElectionBucketName = "CHORIA_AJ_ELECTIONS"
)

// for tests
var defaultBlockedNakTime = 5 * time.Second

type jetStreamStorage struct {
	nc  *nats.Conn
	mgr *jsm.Manager

	tasks           *taskStorage
	configBucket    nats.KeyValue
	leaderElections nats.KeyValue
	retry           RetryPolicyProvider

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

func newJetStreamStorage(nc *nats.Conn, rp RetryPolicyProvider, log Logger) (*jetStreamStorage, error) {
	if nc == nil {
		return nil, ErrNoNatsConn
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

func (s *jetStreamStorage) PublishLeaderElectedEvent(ctx context.Context, name string, component string) error {
	e, err := NewLeaderElectedEvent(name, component)
	if err != nil {
		return err
	}
	ej, err := json.Marshal(e)
	if err != nil {
		return err
	}

	target := fmt.Sprintf(LeaderElectedEventSubjectPattern, component)
	s.log.Debugf("Publishing lifecycle event %s for %s of component type %s", e.EventType, name, component)
	return s.nc.Publish(target, ej)
}

func (s *jetStreamStorage) PublishTaskStateChangeEvent(ctx context.Context, task *Task) error {
	e, err := NewTaskStateChangeEvent(task)
	if err != nil {
		return err
	}

	ej, err := json.Marshal(e)
	if err != nil {
		return err
	}

	target := fmt.Sprintf(TaskStateChangeEventSubjectPattern, task.ID)
	s.log.Debugf("Publishing lifecycle event %s for task %s to %s", e.EventType, task.ID, target)
	return s.nc.Publish(target, ej)
}

func (s *jetStreamStorage) SaveTaskState(ctx context.Context, task *Task, notify bool) error {
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

	if notify {
		return s.PublishTaskStateChangeEvent(ctx, task)
	}

	return nil

}

func (s *jetStreamStorage) RetryTaskByID(ctx context.Context, queue *Queue, id string) error {
	task, err := s.LoadTaskByID(id)
	if err != nil {
		return err
	}

	task.State = TaskStateRetry
	task.Result = nil

	return s.EnqueueTask(ctx, queue, task)
}

func (s *jetStreamStorage) EnqueueTask(ctx context.Context, queue *Queue, task *Task) error {
	if task.State != TaskStateNew && task.State != TaskStateRetry && task.State != TaskStateBlocked {
		return fmt.Errorf("%w %q", ErrTaskTypeCannotEnqueue, task.State)
	}

	ji, err := newProcessItem(TaskItem, task.ID)
	if err != nil {
		return err
	}

	task.Queue = queue.Name

	err = s.SaveTaskState(ctx, task, true)
	if err != nil {
		return err
	}

	msg := nats.NewMsg(fmt.Sprintf(WorkStreamSubjectPattern, queue.Name, task.ID))
	msg.Data = ji

	// if someone is retrying a task we should allow that without dupe checking since they
	// would have removed the work queue item already
	if task.State != TaskStateRetry {
		msg.Header.Add(api.JSMsgId, task.ID) // dedupe on the queue, though should not be needed
	}

	s.log.Debugf("Enqueueing task into queue %s via %s", task.Queue, msg.Subject)
	ret, err := s.nc.RequestMsgWithContext(ctx, msg)
	if err != nil {
		enqueueErrorCounter.WithLabelValues(queue.Name).Inc()
		task.State = TaskStateQueueError
		task.LastErr = err.Error()
		if err := s.SaveTaskState(ctx, task, true); err != nil {
			return err
		}
		if err == nats.ErrNoResponders {
			return fmt.Errorf("%w: %v", ErrQueueNotFound, err)
		}
		return err
	}

	ack, err := jsm.ParsePubAck(ret)
	if err != nil {
		enqueueErrorCounter.WithLabelValues(queue.Name).Inc()
		task.State = TaskStateQueueError
		task.LastErr = err.Error()
		if err := s.SaveTaskState(ctx, task, true); err != nil {
			return err
		}
		return err
	}
	if ack.Duplicate {
		enqueueErrorCounter.WithLabelValues(queue.Name).Inc()
		task.State = TaskStateQueueError
		task.LastErr = err.Error()
		if err := s.SaveTaskState(ctx, task, true); err != nil {
			return err
		}
		return ErrDuplicateItem
	}

	enqueueCounter.WithLabelValues(queue.Name).Inc()

	return nil
}

func (s *jetStreamStorage) AckItem(ctx context.Context, item *ProcessItem) error {
	if item.storageMeta == nil {
		return ErrInvalidStorageItem
	}

	return item.storageMeta.(*nats.Msg).Ack(nats.Context(ctx))
}

func (s *jetStreamStorage) TerminateItem(ctx context.Context, item *ProcessItem) error {
	if item.storageMeta == nil {
		return ErrInvalidStorageItem
	}

	return item.storageMeta.(*nats.Msg).Term(nats.Context(ctx))
}

func (s *jetStreamStorage) NakBlockedItem(ctx context.Context, item *ProcessItem) error {
	if item.storageMeta == nil {
		return ErrInvalidStorageItem
	}

	msg := item.storageMeta.(*nats.Msg)

	timeout, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	s.log.Debugf("NaKing blocked item with 5s delay")

	resp := fmt.Sprintf(`%s {"delay": %d}`, api.AckNak, defaultBlockedNakTime)
	_, err := s.nc.RequestWithContext(timeout, msg.Reply, []byte(resp))

	return err
}

func (s *jetStreamStorage) NakItem(ctx context.Context, item *ProcessItem) error {
	if item.storageMeta == nil {
		return ErrInvalidStorageItem
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
		return nil, ErrInvalidQueueState
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, ErrContextWithoutDeadline
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
		msg.Term(nats.Context(ctx)) // data is corrupt so we terminate it, no associated job to update
		return nil, ErrQueueItemInvalid
	}

	item := &ProcessItem{storageMeta: msg}
	err = json.Unmarshal(msg.Data, item)
	if err != nil || item.JobID == "" {
		workQueueEntryCorruptCounter.WithLabelValues(q.Name).Inc()
		msg.Term(nats.Context(ctx)) // data is corrupt so we terminate it, no associated job to update
		return nil, ErrQueueItemCorrupt
	}

	return item, nil
}

func (s *jetStreamStorage) createQueue(q *Queue, replicas int, memory bool) error {
	if q.MaxTries == 0 {
		q.MaxTries = -1
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
		jsm.MaxMessagesPerSubject(1),
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
	if q.MaxBytes > -1 {
		opts = append(opts, jsm.MaxBytes(q.MaxBytes))
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
		return ErrQueueNotFound
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
			return ErrQueueNotFound
		}
		return err
	}

	s.qConsumers[q.Name], err = s.qStreams[q.Name].LoadConsumer("WORKERS")
	if err != nil {
		if jsm.IsNatsError(err, 10014) {
			return ErrQueueConsumerNotFound
		}
		return err
	}

	return s.updateQueueSettings(q)
}

func (s *jetStreamStorage) PrepareQueue(q *Queue, replicas int, memory bool) error {
	if q.Name == "" {
		return ErrQueueNameRequired
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if q.NoCreate {
		return s.joinQueue(q)
	}

	return s.createQueue(q, replicas, memory)
}

func (s *jetStreamStorage) ConfigurationInfo() (*nats.KeyValueBucketStatus, error) {
	if s.configBucket == nil {
		return nil, fmt.Errorf("%w: configuration bucket not configured", ErrStorageNotReady)
	}

	st, err := s.configBucket.Status()
	if err != nil {
		return nil, err
	}

	return st.(*nats.KeyValueBucketStatus), nil
}

func (s *jetStreamStorage) TasksInfo() (*TasksInfo, error) {
	if s.tasks == nil || s.tasks.stream == nil {
		return nil, fmt.Errorf("%w: task store not initialized", ErrStorageNotReady)
	}

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
	if s.tasks == nil || s.tasks.stream == nil {
		return fmt.Errorf("%w: task store not initialized", ErrStorageNotReady)
	}

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
	if s.tasks == nil || s.tasks.stream == nil {
		return nil, fmt.Errorf("%w: task store not initialized", ErrStorageNotReady)
	}

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

func (s *jetStreamStorage) DeleteScheduledTaskByName(name string) error {
	if s.configBucket == nil {
		return fmt.Errorf("%w: scheduled storage not prepared", ErrStorageNotReady)
	}

	return s.configBucket.Delete(fmt.Sprintf("scheduled_tasks.%s", name))
}

func (s *jetStreamStorage) ScheduledTasks(ctx context.Context) ([]*ScheduledTask, error) {
	if s.configBucket == nil {
		return nil, fmt.Errorf("%w: scheduled storage not prepared", ErrStorageNotReady)
	}

	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	watch, err := s.configBucket.Watch("scheduled_tasks.*", nats.Context(wctx))
	if err != nil {
		return nil, err
	}

	var tasks []*ScheduledTask

	for {
		select {
		case entry := <-watch.Updates():
			if entry == nil {
				cancel()
				return tasks, nil
			}

			if entry.Operation() != nats.KeyValuePut {
				continue
			}

			st := &ScheduledTask{}
			err = json.Unmarshal(entry.Value(), st)
			if err != nil {
				s.log.Errorf("could not process received scheduled task: %v", err)
				continue
			}

			tasks = append(tasks, st)

		case <-wctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (s *jetStreamStorage) ScheduledTasksWatch(ctx context.Context) (chan *ScheduleWatchEntry, error) {
	if s.configBucket == nil {
		return nil, fmt.Errorf("%w: scheduled storage not prepared", ErrStorageNotReady)
	}

	watch, err := s.configBucket.Watch("scheduled_tasks.*", nats.Context(ctx))
	if err != nil {
		return nil, err
	}

	tasks := make(chan *ScheduleWatchEntry, 100)

	go func() {
		for {
			select {
			case entry := <-watch.Updates():
				if entry == nil {
					tasks <- nil
					continue
				}

				if entry.Operation() == nats.KeyValueDelete || entry.Operation() == nats.KeyValuePurge {
					parts := strings.Split(entry.Key(), ".")
					tasks <- &ScheduleWatchEntry{
						Name:   parts[len(parts)-1],
						Delete: true,
					}
					continue
				}

				st := &ScheduledTask{}
				err = json.Unmarshal(entry.Value(), st)
				if err != nil {
					s.log.Errorf("Could not process received scheduled task: %v", err)
					continue
				}

				tasks <- &ScheduleWatchEntry{
					Name: st.Name,
					Task: st,
				}

			case <-ctx.Done():
				s.log.Debugf("Closing watcher due to context interrupt")
				close(tasks)
				return
			}
		}
	}()

	return tasks, nil
}

func (s *jetStreamStorage) LoadScheduledTaskByName(name string) (*ScheduledTask, error) {
	if s.configBucket == nil {
		return nil, fmt.Errorf("%w: scheduled storage not prepared", ErrStorageNotReady)
	}

	e, err := s.configBucket.Get(fmt.Sprintf("scheduled_tasks.%s", name))
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return nil, ErrScheduledTaskNotFound
		}

		return nil, err
	}

	st := &ScheduledTask{}
	err = json.Unmarshal(e.Value(), st)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrScheduledTaskInvalid, err)
	}

	return st, nil
}

func (s *jetStreamStorage) SaveScheduledTask(st *ScheduledTask, update bool) error {
	if s.configBucket == nil {
		return fmt.Errorf("%w: scheduled storage not prepared", ErrStorageNotReady)
	}

	stj, err := json.Marshal(st)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("scheduled_tasks.%s", st.Name)
	var rev uint64

	if update {
		rev, err = s.configBucket.Put(key, stj)
	} else {
		rev, err = s.configBucket.Create(key, stj)
	}
	if err != nil {
		if strings.Contains(err.Error(), "wrong last sequence") {
			return ErrScheduledTaskAlreadyExist
		}
		return err
	}

	s.log.Debugf("Stored scheduled task %s in revision %d", key, rev)

	return nil
}

func (s *jetStreamStorage) PrepareConfigurationStore(memory bool, replicas int, maxBytes int64, maxBytesSet bool) error {
	var err error

	if replicas == 0 {
		replicas = 1
	}

	js, err := s.nc.JetStream()
	if err != nil {
		return err
	}

	storage := nats.FileStorage
	if memory {
		storage = nats.MemoryStorage
	}

	kv, err := js.KeyValue(ConfigBucketName)
	if err == nats.ErrBucketNotFound {
		cfg := &nats.KeyValueConfig{
			Bucket:      ConfigBucketName,
			Description: "Choria Async Jobs Configuration",
			Storage:     storage,
			Replicas:    replicas,
		}

		if maxBytes > -1 {
			cfg.MaxBytes = maxBytes
		}

		kv, err = js.CreateKeyValue(cfg)
	}
	if err != nil {
		return err
	}

	s.configBucket = kv

	kv, err = js.KeyValue(LeaderElectionBucketName)
	if err == nats.ErrBucketNotFound {
		cfg := &nats.KeyValueConfig{
			Bucket:      LeaderElectionBucketName,
			Description: "Choria Async Jobs Leader Elections",
			Storage:     storage,
			Replicas:    replicas,
			TTL:         10 * time.Second,
		}

		if maxBytesSet {
			cfg.MaxBytes = maxBytes
		}

		kv, err = js.CreateKeyValue(cfg)
	}
	if err != nil {
		return err
	}

	s.leaderElections = kv

	return nil

}

func (s *jetStreamStorage) PrepareTasks(memory bool, replicas int, retention time.Duration, maxBytes int64, maxBytesSet bool) error {
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

	if maxBytesSet {
		opts = append(opts, jsm.MaxBytes(maxBytes))
	}

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

// ElectionStorage gives access to the key-value store used for elections
func (s *jetStreamStorage) ElectionStorage() (nats.KeyValue, error) {
	if s.leaderElections == nil {
		return nil, fmt.Errorf("%s: election bucket not configured", ErrStorageNotReady)
	}

	return s.leaderElections, nil
}

func (s *jetStreamStorage) Tasks(ctx context.Context, limit int32) (chan *Task, error) {
	if s.tasks == nil || s.tasks.stream == nil {
		return nil, fmt.Errorf("%w: task store not initialized", ErrStorageNotReady)
	}

	if limit <= 0 {
		s.log.Debugf("Defaulting to task list limit of 10000")
		limit = 10000
	}

	nfo, err := s.tasks.stream.State()
	if err != nil {
		return nil, err
	}
	if nfo.Msgs == 0 {
		return nil, ErrNoTasks
	}

	out := make(chan *Task, limit)
	cnt := int32(0)

	timeout, cancel := context.WithTimeout(ctx, time.Minute)

	sub, err := s.nc.Subscribe(s.nc.NewRespInbox(), func(msg *nats.Msg) {
		if len(msg.Data) == 0 {
			return
		}

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

		msg.Ack()
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
	if len(data) == 0 {
		return map[string][]string{}, nil
	}

	tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(data)))
	if l, err := tp.ReadLine(); err != nil || l != hdrLine[:hdrPreEnd] {
		return nil, ErrInvalidHeaders
	}

	mh, err := tp.ReadMIMEHeader()
	if err != nil {
		return nil, ErrInvalidHeaders
	}

	return http.Header(mh), nil
}
