// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Storage", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
	})

	AfterEach(func() { cancel() })

	testQueue := func() *Queue {
		return &Queue{Name: "ginkgo"}
	}

	Describe("PrepareTasks", func() {
		It("Should support memory", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour)
				Expect(err).ToNot(HaveOccurred())

				Expect(storage.tasks.stream.Storage()).To(Equal(api.MemoryStorage))
			})
		})

		It("Should support retention", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour)
				Expect(err).ToNot(HaveOccurred())

				Expect(storage.tasks.stream.MaxAge()).To(Equal(time.Hour))
			})
		})

		It("Should create the task store correctly", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(false, 1, 0)
				Expect(err).ToNot(HaveOccurred())

				stream := storage.tasks.stream
				Expect(stream.MaxAge()).To(Equal(time.Duration(0)))
				Expect(stream.Name()).To(Equal(TasksStreamName))
				Expect(stream.Subjects()).To(Equal([]string{TasksStreamSubjects}))
				Expect(stream.MaxMsgsPerSubject()).To(Equal(int64(1)))
				Expect(stream.Storage()).To(Equal(api.FileStorage))
			})
		})
	})

	Describe("LoadTaskByID", func() {
		It("Should read the correct task", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())
				err = storage.PrepareTasks(true, 1, time.Hour)
				Expect(err).ToNot(HaveOccurred())

				task1, err := NewTask("ginkgo", "test")
				Expect(err).ToNot(HaveOccurred())
				err = storage.EnqueueTask(ctx, q, task1)
				Expect(err).ToNot(HaveOccurred())

				task2, err := NewTask("ginkgo", "test")
				Expect(err).ToNot(HaveOccurred())
				err = storage.EnqueueTask(ctx, q, task2)
				Expect(err).ToNot(HaveOccurred())

				t, err := storage.LoadTaskByID(task1.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(t.ID).To(Equal(task1.ID))

				t, err = storage.LoadTaskByID(task2.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(t.ID).To(Equal(task2.ID))

				Expect(t.storageOptions).ToNot(BeNil())
			})
		})
	})

	Describe("PrepareQueue", func() {
		prepare := func(cb func(storage *jetStreamStorage, q *Queue)) {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				cb(storage, testQueue())
			})
		}

		It("Should require a name", func() {
			prepare(func(storage *jetStreamStorage, q *Queue) {
				q.Name = ""
				err := storage.PrepareQueue(q, 1, true)
				Expect(err).To(MatchError("name is required"))
			})
		})

		It("Should set defaults", func() {
			prepare(func(storage *jetStreamStorage, q *Queue) {
				q = &Queue{Name: "ginkgo"}
				err := storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				Expect(q.Priority).To(Equal(DefaultPriority))
				Expect(q.MaxTries).To(Equal(DefaultMaxTries))
				Expect(q.MaxRunTime).To(Equal(DefaultJobRunTime))
				Expect(q.MaxConcurrent).To(Equal(DefaultQueueMaxConcurrent))
			})
		})

		It("Should detect invalid priority", func() {
			prepare(func(storage *jetStreamStorage, q *Queue) {
				q = &Queue{Name: "ginkgo", Priority: 100}
				err := storage.PrepareQueue(q, 1, true)
				Expect(err).To(MatchError("invalid priority 100 on queue ginkgo, must be between 1 and 10"))

				q.Priority = -1
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).To(MatchError("invalid priority -1 on queue ginkgo, must be between 1 and 10"))
			})
		})

		It("Should support memory storage", func() {
			prepare(func(storage *jetStreamStorage, q *Queue) {
				err := storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				stream := storage.qStreams[q.Name]
				Expect(stream.Storage()).To(Equal(api.MemoryStorage))
			})
		})

		It("Should support a max age", func() {
			prepare(func(storage *jetStreamStorage, q *Queue) {
				q.MaxAge = time.Hour
				err := storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				stream := storage.qStreams[q.Name]
				Expect(stream.MaxAge()).To(Equal(time.Hour))
			})
		})

		It("Should support max entries", func() {
			prepare(func(storage *jetStreamStorage, q *Queue) {
				q.MaxEntries = 1000
				err := storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				stream := storage.qStreams[q.Name]
				Expect(stream.MaxMsgs()).To(Equal(int64(1000)))
			})
		})

		It("Should support discard old", func() {
			prepare(func(storage *jetStreamStorage, q *Queue) {
				q.DiscardOld = true
				err := storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				stream := storage.qStreams[q.Name]
				Expect(stream.Configuration().Discard).To(Equal(api.DiscardOld))
			})
		})

		It("Should support max run time", func() {
			prepare(func(storage *jetStreamStorage, q *Queue) {
				q.MaxRunTime = time.Hour
				err := storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				consumer := storage.qConsumers[q.Name]
				Expect(consumer.AckWait()).To(Equal(time.Hour))
			})
		})

		It("Should support max concurrent", func() {
			prepare(func(storage *jetStreamStorage, q *Queue) {
				q.MaxConcurrent = 200
				err := storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				consumer := storage.qConsumers[q.Name]
				Expect(consumer.MaxAckPending()).To(Equal(200))
			})
		})

		It("Should support max tries", func() {
			prepare(func(storage *jetStreamStorage, q *Queue) {
				q.MaxTries = 111
				err := storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				consumer := storage.qConsumers[q.Name]
				Expect(consumer.MaxDeliver()).To(Equal(111))
			})
		})

		It("Should create stream and consumers correctly", func() {
			prepare(func(storage *jetStreamStorage, q *Queue) {
				err := storage.PrepareQueue(q, 1, false)
				Expect(err).ToNot(HaveOccurred())

				consumer := storage.qConsumers[q.Name]
				stream := storage.qStreams[q.Name]

				Expect(stream.Name()).To(Equal(fmt.Sprintf(WorkStreamNamePattern, q.Name)))
				Expect(stream.Subjects()).To(Equal([]string{fmt.Sprintf(WorkStreamSubjectPattern, q.Name)}))
				Expect(stream.Retention()).To(Equal(api.WorkQueuePolicy))
				Expect(stream.Storage()).To(Equal(api.FileStorage))

				Expect(consumer.Name()).To(Equal("WORKERS"))
				Expect(consumer.DurableName()).To(Equal("WORKERS"))
				Expect(consumer.AckPolicy()).To(Equal(api.AckExplicit))
			})
		})
	})

	Describe("PollQueue", func() {
		It("Should fail for unknown queues", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				item, err := storage.PollQueue(ctx, &defaultQueue)
				Expect(item).To(BeNil())
				Expect(err).To(MatchError("invalid queue storage state"))
			})
		})

		It("Should handle empty queues", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				ts := time.Now()
				item, err := storage.PollQueue(ctx, q)
				Expect(item).To(BeNil())
				Expect(err).To(BeNil())
				Expect(ts).To(BeTemporally("~", time.Now(), 50*time.Millisecond)) // should fail fast due to nowait
			})
		})

		It("Should handle invalid items", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				stream := storage.qStreams[q.Name]

				// nil body items
				_, err = nc.Request(stream.Subjects()[0], nil, time.Second)
				Expect(err).ToNot(HaveOccurred())

				item, err := storage.PollQueue(ctx, q)
				Expect(item).To(BeNil())
				Expect(err).To(MatchError("invalid queue item received"))

				// should have been termed
				nfo, err := stream.Information()
				Expect(err).ToNot(HaveOccurred())
				Expect(nfo.State.Msgs).To(Equal(uint64(0)))

				// invalid body item
				_, err = nc.Request(stream.Subjects()[0], []byte("foo"), time.Second)
				Expect(err).ToNot(HaveOccurred())

				item, err = storage.PollQueue(ctx, q)
				Expect(item).To(BeNil())
				Expect(err).To(MatchError("corrupt queue item received"))

				// should have been termed
				nfo, err = stream.Information()
				Expect(err).ToNot(HaveOccurred())
				Expect(nfo.State.Msgs).To(Equal(uint64(0)))
			})
		})

		It("Should poll correctly", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())
				err = storage.PrepareTasks(true, 1, time.Hour)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test")
				Expect(err).ToNot(HaveOccurred())

				err = storage.EnqueueTask(ctx, q, task)
				Expect(err).ToNot(HaveOccurred())

				item, err := storage.PollQueue(ctx, q)
				Expect(err).ToNot(HaveOccurred())

				Expect(item.JobID).To(Equal(task.ID))
				Expect(item.Kind).To(Equal(TaskItem))
				Expect(item.storageMeta).ToNot(BeNil())
			})
		})
	})

	Describe("NaKItem", func() {
		It("Should fail for invalid items", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				item := &ProcessItem{Kind: TaskItem, JobID: "123"}
				err = storage.NakItem(ctx, item)
				Expect(err).To(MatchError("invalid storage item"))
			})
		})

		It("Should NaK with delay", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())
				err = storage.PrepareTasks(true, 1, time.Hour)
				Expect(err).ToNot(HaveOccurred())
				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.EnqueueTask(ctx, q, task)
				Expect(err).ToNot(HaveOccurred())

				item, err := storage.PollQueue(ctx, q)
				Expect(err).ToNot(HaveOccurred())
				Expect(item.JobID).To(Equal(task.ID))

				msg := item.storageMeta.(*nats.Msg)
				sub, err := nc.SubscribeSync(msg.Reply)
				Expect(err).ToNot(HaveOccurred())

				err = storage.NakItem(ctx, item)
				Expect(err).ToNot(HaveOccurred())

				ack, err := sub.NextMsg(time.Second)
				Expect(err).ToNot(HaveOccurred())
				Expect(bytes.Contains(ack.Data, []byte("-NAK {\"delay\":"))).To(BeTrue())
			})
		})
	})

	Describe("AckItem", func() {
		It("Should fail when the item has no storage metadata", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				item := &ProcessItem{Kind: TaskItem, JobID: "123"}
				err = storage.AckItem(ctx, item)
				Expect(err).To(MatchError("invalid storage item"))
			})
		})

		It("Should ack the item", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())
				err = storage.PrepareTasks(true, 1, time.Hour)
				Expect(err).ToNot(HaveOccurred())
				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.EnqueueTask(ctx, q, task)
				Expect(err).ToNot(HaveOccurred())

				item, err := storage.PollQueue(ctx, q)
				Expect(err).ToNot(HaveOccurred())
				Expect(item.JobID).To(Equal(task.ID))

				nfo, err := storage.qStreams[q.Name].Information()
				Expect(err).ToNot(HaveOccurred())
				Expect(nfo.State.Msgs).To(Equal(uint64(1)))

				err = storage.AckItem(ctx, item)
				Expect(err).ToNot(HaveOccurred())

				nfo, err = storage.qStreams[q.Name].Information()
				Expect(err).ToNot(HaveOccurred())
				Expect(nfo.State.Msgs).To(Equal(uint64(0)))
			})
		})
	})

	Describe("EnqueueTask", func() {
		It("Save the task and handle save errors", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.EnqueueTask(ctx, q, task)
				Expect(err).To(Equal(nats.ErrNoResponders))
			})
		})

		It("Should create the WQ entry with dedupe headers", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour)
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.EnqueueTask(ctx, q, task)
				Expect(err).ToNot(HaveOccurred())

				msg, err := storage.qStreams[q.Name].ReadMessage(1)
				Expect(err).ToNot(HaveOccurred())
				header, err := decodeHeadersMsg(msg.Header)
				Expect(err).ToNot(HaveOccurred())
				Expect(header.Get(api.JSMsgId)).To(Equal(task.ID))
			})
		})

		It("Should enqueue task and job correctly", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour)
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.EnqueueTask(ctx, q, task)
				Expect(err).ToNot(HaveOccurred())

				msg, err := storage.qStreams[q.Name].ReadMessage(1)
				Expect(err).ToNot(HaveOccurred())

				item := ProcessItem{}
				err = json.Unmarshal(msg.Data, &item)
				Expect(err).ToNot(HaveOccurred())
				Expect(item.Kind).To(Equal(TaskItem))
				Expect(item.JobID).To(Equal(task.ID))

				_, err = storage.LoadTaskByID(task.ID)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	Describe("SaveTaskState", func() {
		It("Should handle missing streams", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.SaveTaskState(ctx, task)
				if err != nats.ErrNoResponders {
					Fail(fmt.Sprintf("Unexpected error: %v", err))
				}
			})
		})

		It("Should use the correct last subject sequence for new saves", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.SaveTaskState(ctx, task)
				Expect(err).ToNot(HaveOccurred())

				tasks, err := mgr.LoadStream(TasksStreamName)
				Expect(err).ToNot(HaveOccurred())

				msg, err := tasks.ReadLastMessageForSubject(fmt.Sprintf(TasksStreamSubjectPattern, task.ID))
				Expect(err).ToNot(HaveOccurred())
				header, err := decodeHeadersMsg(msg.Header)
				Expect(err).ToNot(HaveOccurred())
				Expect(header.Get(api.JSExpectedLastSubjSeq)).To(Equal("0"))
			})
		})

		It("Should use the correct last subject sequence for updates", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.SaveTaskState(ctx, task)
				Expect(err).ToNot(HaveOccurred())
				Expect(task.storageOptions).ToNot(BeNil())
				so := task.storageOptions.(*taskMeta)
				Expect(so.seq).To(Equal(uint64(1)))

				tasks, err := mgr.LoadStream(TasksStreamName)
				Expect(err).ToNot(HaveOccurred())

				msg, err := tasks.ReadLastMessageForSubject(fmt.Sprintf(TasksStreamSubjectPattern, task.ID))
				Expect(err).ToNot(HaveOccurred())
				header, err := decodeHeadersMsg(msg.Header)
				Expect(err).ToNot(HaveOccurred())
				Expect(header.Get(api.JSExpectedLastSubjSeq)).To(Equal("0"))
				Expect(msg.Sequence).To(Equal(uint64(1)))

				err = storage.SaveTaskState(ctx, task)
				Expect(err).ToNot(HaveOccurred())
				Expect(task.storageOptions).ToNot(BeNil())
				so = task.storageOptions.(*taskMeta)
				Expect(so.seq).To(Equal(uint64(2)))

				msg, err = tasks.ReadLastMessageForSubject(fmt.Sprintf(TasksStreamSubjectPattern, task.ID))
				Expect(err).ToNot(HaveOccurred())
				header, err = decodeHeadersMsg(msg.Header)
				Expect(err).ToNot(HaveOccurred())
				Expect(header.Get(api.JSExpectedLastSubjSeq)).To(Equal("1"))
			})
		})

		It("Should handle publish failures based on the last subject sequence", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.SaveTaskState(ctx, task)
				Expect(err).ToNot(HaveOccurred())

				// force the failure
				task.storageOptions.(*taskMeta).seq = 10

				err = storage.SaveTaskState(ctx, task)
				if !jsm.IsNatsError(err, 10071) {
					Fail(fmt.Sprintf("Unexpected error %v", err))
				}
			})
		})

		It("Should save the task correctly", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting)
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.SaveTaskState(ctx, task)
				Expect(err).ToNot(HaveOccurred())

				lt, err := storage.LoadTaskByID(task.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(lt.ID).To(Equal(task.ID))
				Expect(lt.CreatedAt).To(Equal(task.CreatedAt))
				Expect(lt.Type).To(Equal(task.Type))

				Expect(task.storageOptions).ToNot(BeNil())
				so := task.storageOptions.(*taskMeta)
				Expect(so.seq).To(Equal(uint64(1)))
			})
		})
	})
})
