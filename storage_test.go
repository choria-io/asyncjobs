// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
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
		ctx, cancel = context.WithTimeout(context.Background(), time.Minute)
		log.SetOutput(GinkgoWriter)
	})

	AfterEach(func() { cancel() })

	testQueue := func() *Queue {
		return &Queue{Name: "ginkgo"}
	}

	Describe("ScheduledTasksWatch", func() {
		It("Should handle no tasks", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareConfigurationStore(true, 1, -1, false)
				Expect(err).ToNot(HaveOccurred())

				wctx, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()

				tasks, err := storage.ScheduledTasksWatch(wctx)
				Expect(err).ToNot(HaveOccurred())

				task := <-tasks
				Expect(task).To(BeNil())
			})
		})

		It("Should handle some tasks and nil at the end and then send updates", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareConfigurationStore(true, 1, -1, false)
				Expect(err).ToNot(HaveOccurred())

				for i := 0; i < 2; i++ {
					st, _, err := newScheduledTask(fmt.Sprintf("scheduled_%d", i), "@daily", "DEFAULT", "email:new", nil)
					Expect(err).ToNot(HaveOccurred())

					err = storage.SaveScheduledTask(st, true)
					Expect(err).ToNot(HaveOccurred())
				}

				wctx, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()

				tasks, err := storage.ScheduledTasksWatch(wctx)
				Expect(err).ToNot(HaveOccurred())

				task := <-tasks
				Expect(task.Name).To(Equal("scheduled_0"))
				Expect(task.Task.Name).To(Equal("scheduled_0"))
				Expect(task.Delete).To(BeFalse())
				task = <-tasks
				Expect(task.Name).To(Equal("scheduled_1"))
				Expect(task.Task.Name).To(Equal("scheduled_1"))
				Expect(task.Delete).To(BeFalse())
				task = <-tasks
				Expect(task).To(BeNil())

				st, _, err := newScheduledTask("scheduled_new", "@daily", "DEFAULT", "email:new", nil)
				Expect(err).ToNot(HaveOccurred())
				err = storage.SaveScheduledTask(st, true)
				Expect(err).ToNot(HaveOccurred())

				task = <-tasks
				Expect(task.Name).To(Equal("scheduled_new"))
				Expect(task.Task.Name).To(Equal("scheduled_new"))
				Expect(task.Delete).To(BeFalse())

				storage.DeleteScheduledTaskByName("scheduled_new")
				task = <-tasks
				Expect(task.Name).To(Equal("scheduled_new"))
				Expect(task.Task).To(BeNil())
				Expect(task.Delete).To(BeTrue())
			})
		})
	})

	Describe("ScheduledTasks", func() {
		It("Should handle no tasks", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareConfigurationStore(true, 1, -1, false)
				Expect(err).ToNot(HaveOccurred())

				tasks, err := storage.ScheduledTasks(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(tasks).To(HaveLen(0))
			})
		})

		It("Should return found tasks", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareConfigurationStore(true, 1, -1, false)
				Expect(err).ToNot(HaveOccurred())

				for i := 0; i < 10; i++ {
					st, _, err := newScheduledTask(fmt.Sprintf("scheduled_%d", i), "@daily", "DEFAULT", "email:new", nil)
					Expect(err).ToNot(HaveOccurred())

					err = storage.SaveScheduledTask(st, true)
					Expect(err).ToNot(HaveOccurred())
				}

				tasks, err := storage.ScheduledTasks(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(tasks).To(HaveLen(10))
				Expect(tasks[0].Name).To(Equal("scheduled_0"))
				Expect(tasks[9].Name).To(Equal("scheduled_9"))
			})
		})
	})

	Describe("LoadScheduledTaskByName", func() {
		It("Should handle corrupt tasks", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareConfigurationStore(true, 1, -1, false)
				Expect(err).ToNot(HaveOccurred())

				_, err = storage.configBucket.Put("scheduled_tasks.test", []byte("{invalid"))
				Expect(err).ToNot(HaveOccurred())

				_, err = storage.LoadScheduledTaskByName("test")
				Expect(err).To(MatchError(ErrScheduledTaskInvalid))
			})
		})

		It("Should handle missing tasks", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareConfigurationStore(true, 1, -1, false)
				Expect(err).ToNot(HaveOccurred())

				_, err = storage.LoadScheduledTaskByName("test")
				Expect(err).To(MatchError(ErrScheduledTaskNotFound))
			})
		})

		It("Should load the task", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareConfigurationStore(true, 1, -1, false)
				Expect(err).ToNot(HaveOccurred())

				st, _, err := newScheduledTask("daily", "@daily", "DEFAULT", "email:new", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.SaveScheduledTask(st, true)
				Expect(err).ToNot(HaveOccurred())

				lst, err := storage.LoadScheduledTaskByName("daily")
				Expect(err).ToNot(HaveOccurred())

				Expect(lst).To(Equal(st))
			})
		})
	})

	Describe("SaveScheduledTask", func() {
		It("Should support updating a task", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareConfigurationStore(true, 1, -1, false)
				Expect(err).ToNot(HaveOccurred())

				st, _, err := newScheduledTask("daily", "@daily", "DEFAULT", "email:new", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.SaveScheduledTask(st, true)
				Expect(err).ToNot(HaveOccurred())

				e, err := storage.configBucket.Get(fmt.Sprintf("scheduled_tasks.%s", st.Name))
				Expect(err).ToNot(HaveOccurred())
				st = &ScheduledTask{}
				err = json.Unmarshal(e.Value(), st)
				Expect(err).ToNot(HaveOccurred())
				Expect(st.Name).To(Equal("daily"))
			})
		})

		It("Should support creating the task", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareConfigurationStore(true, 1, -1, false)
				Expect(err).ToNot(HaveOccurred())

				st, _, err := newScheduledTask("daily", "@daily", "DEFAULT", "email:new", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.SaveScheduledTask(st, true)
				Expect(err).ToNot(HaveOccurred())

				err = storage.SaveScheduledTask(st, false)
				Expect(err).To(MatchError(ErrScheduledTaskAlreadyExist))
			})
		})
	})

	Describe("PrepareConfigurationStore", func() {
		It("Should support memory", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareConfigurationStore(true, 1, -1, false)
				Expect(err).ToNot(HaveOccurred())

				kvs, err := storage.configBucket.Status()
				Expect(err).ToNot(HaveOccurred())

				Expect(kvs.(*nats.KeyValueBucketStatus).StreamInfo().Config.Storage).To(Equal(nats.MemoryStorage))
			})
		})

		It("Should create the scheduled task store correctly", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareConfigurationStore(false, 1, -1, false)
				Expect(err).ToNot(HaveOccurred())

				kvs, err := storage.configBucket.Status()
				Expect(err).ToNot(HaveOccurred())
				stream := kvs.(*nats.KeyValueBucketStatus).StreamInfo().Config
				Expect(stream.MaxMsgsPerSubject).To(Equal(int64(1)))
				Expect(stream.Storage).To(Equal(nats.FileStorage))
			})
		})
	})

	Describe("PrepareTasks", func() {
		It("Should support memory", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour, -1, false)
				Expect(err).ToNot(HaveOccurred())

				Expect(storage.tasks.stream.Storage()).To(Equal(api.MemoryStorage))
			})
		})

		It("Should support retention", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour, -1, false)
				Expect(err).ToNot(HaveOccurred())

				Expect(storage.tasks.stream.MaxAge()).To(Equal(time.Hour))
			})
		})

		It("Should create the task store correctly", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(false, 1, 0, -1, false)
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
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())
				err = storage.PrepareTasks(true, 1, time.Hour, -1, false)
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
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				cb(storage, testQueue())
			})
		}

		It("Should require a name", func() {
			prepare(func(storage *jetStreamStorage, q *Queue) {
				q.Name = ""
				err := storage.PrepareQueue(q, 1, true)
				Expect(err).To(Equal(ErrQueueNameRequired))
			})
		})

		It("Should set defaults", func() {
			prepare(func(storage *jetStreamStorage, _ *Queue) {
				q := &Queue{Name: "ginkgo"}
				err := storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				Expect(q.MaxTries).To(Equal(-1))
				Expect(q.MaxRunTime).To(Equal(DefaultJobRunTime))
				Expect(q.MaxConcurrent).To(Equal(DefaultQueueMaxConcurrent))
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
				Expect(stream.Subjects()).To(Equal([]string{fmt.Sprintf(WorkStreamSubjectPattern, q.Name, ">")}))
				Expect(stream.Retention()).To(Equal(api.WorkQueuePolicy))
				Expect(stream.Storage()).To(Equal(api.FileStorage))

				Expect(consumer.Name()).To(Equal("WORKERS"))
				Expect(consumer.DurableName()).To(Equal("WORKERS"))
				Expect(consumer.AckPolicy()).To(Equal(api.AckExplicit))
			})
		})

		It("Should support joining an existing queue", func() {
			prepare(func(storage *jetStreamStorage, q *Queue) {
				q.NoCreate = true
				err := storage.PrepareQueue(q, 1, false)
				Expect(err).To(Equal(ErrQueueNotFound))

				q.NoCreate = false
				err = storage.PrepareQueue(q, 1, false)
				Expect(err).ToNot(HaveOccurred())
				Expect(storage.qConsumers[q.Name].Delete()).ToNot(HaveOccurred())

				q.NoCreate = true
				err = storage.PrepareQueue(q, 1, false)
				Expect(err).To(Equal(ErrQueueConsumerNotFound))
			})
		})
	})

	Describe("PollQueue", func() {
		It("Should fail for unknown queues", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				item, err := storage.PollQueue(ctx, newDefaultQueue())
				Expect(item).To(BeNil())
				Expect(err).To(MatchError("invalid queue storage state"))
			})
		})

		It("Should handle empty queues", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				timeout, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()

				ts := time.Now()
				item, err := storage.PollQueue(timeout, q)
				Expect(item).To(BeNil())
				Expect(ts).To(BeTemporally("~", time.Now(), 2*time.Second))
				Expect(err).To(Equal(context.DeadlineExceeded))
			})
		})

		It("Should handle invalid items", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
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
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())
				err = storage.PrepareTasks(true, 1, time.Hour, -1, false)
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
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				item := &ProcessItem{Kind: TaskItem, JobID: "123"}
				err = storage.NakItem(ctx, item)
				Expect(err).To(MatchError("invalid storage item"))
			})
		})

		It("Should NaK with delay", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())
				err = storage.PrepareTasks(true, 1, time.Hour, -1, false)
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

	Describe("TasksInfo", func() {
		It("Should return the correct info", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())
				Expect(storage.PrepareTasks(true, 1, time.Hour, -1, false)).ToNot(HaveOccurred())

				nfo, err := storage.TasksInfo()
				Expect(err).ToNot(HaveOccurred())

				Expect(nfo.Stream).ToNot(BeNil())
				Expect(nfo.Stream.Config.Name).To(Equal(TasksStreamName))
			})
		})
	})

	Describe("DeleteTaskByID", func() {
		It("Should delete the task", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				q1 := &Queue{Name: "Q1"}
				Expect(storage.PrepareQueue(q1, 1, true)).ToNot(HaveOccurred())
				Expect(storage.PrepareTasks(true, 1, time.Hour, -1, false)).ToNot(HaveOccurred())

				task, err := NewTask("x", "x")
				Expect(err).ToNot(HaveOccurred())
				Expect(storage.EnqueueTask(ctx, q1, task)).ToNot(HaveOccurred())

				nfo, err := storage.TasksInfo()
				Expect(err).ToNot(HaveOccurred())
				Expect(nfo.Stream.State.Msgs).To(Equal(uint64(1)))

				Expect(storage.DeleteTaskByID(task.ID)).ToNot(HaveOccurred())

				nfo, err = storage.TasksInfo()
				Expect(err).ToNot(HaveOccurred())
				Expect(nfo.Stream.State.Msgs).To(Equal(uint64(0)))
			})
		})
	})

	Describe("PurgeQueue", func() {
		It("Should purge all the messages", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				q1 := &Queue{Name: "Q1"}
				Expect(storage.PrepareQueue(q1, 1, true)).ToNot(HaveOccurred())
				Expect(storage.PrepareTasks(true, 1, time.Hour, -1, false)).ToNot(HaveOccurred())

				task, err := NewTask("x", "x")
				Expect(err).ToNot(HaveOccurred())
				Expect(storage.EnqueueTask(ctx, q1, task)).ToNot(HaveOccurred())

				nfo, err := storage.QueueInfo(q1.Name)
				Expect(err).ToNot(HaveOccurred())
				Expect(nfo.Stream.State.Msgs).To(Equal(uint64(1)))

				Expect(storage.PurgeQueue(q1.Name)).ToNot(HaveOccurred())

				nfo, err = storage.QueueInfo(q1.Name)
				Expect(err).ToNot(HaveOccurred())
				Expect(nfo.Stream.State.Msgs).To(Equal(uint64(0)))

			})
		})
	})

	Describe("Queues", func() {
		It("Should find all the queues", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				q1 := &Queue{Name: "Q1"}
				q2 := &Queue{Name: "Q2"}

				Expect(storage.PrepareQueue(q1, 1, true)).ToNot(HaveOccurred())
				Expect(storage.PrepareQueue(q2, 1, true)).ToNot(HaveOccurred())

				queues, err := storage.Queues()
				if err != nil {
					return
				}

				Expect(queues).To(HaveLen(2))
				Expect(queues[0].Name).To(Equal(q1.Name))
				Expect(queues[0].Stream.Config.Name).To(HaveSuffix(q1.Name))
				Expect(queues[0].Consumer.Name).To(Equal("WORKERS"))
				Expect(queues[1].Name).To(Equal(q2.Name))
				Expect(queues[1].Stream.Config.Name).To(HaveSuffix(q2.Name))
				Expect(queues[1].Consumer.Name).To(Equal("WORKERS"))
			})
		})
	})

	Describe("AckItem", func() {
		It("Should fail when the item has no storage metadata", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				item := &ProcessItem{Kind: TaskItem, JobID: "123"}
				err = storage.AckItem(ctx, item)
				Expect(err).To(MatchError("invalid storage item"))
			})
		})

		It("Should ack the item", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())
				err = storage.PrepareTasks(true, 1, time.Hour, -1, false)
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

	Describe("RetryTaskByID", func() {
		It("Should handle missing tasks", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()

				Expect(storage.PrepareTasks(true, 1, time.Hour, -1, false)).ToNot(HaveOccurred())
				Expect(storage.RetryTaskByID(context.Background(), q, "123")).To(Equal(ErrTaskNotFound))
			})
		})

		It("Should remove the task from the queue and enqueue with retry state", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				Expect(storage.PrepareQueue(q, 1, true)).ToNot(HaveOccurred())
				Expect(storage.PrepareTasks(true, 1, time.Hour, -1, false)).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(storage.EnqueueTask(context.Background(), q, task)).ToNot(HaveOccurred())

				nfo, err := storage.qStreams[q.Name].State()
				Expect(err).ToNot(HaveOccurred())
				Expect(nfo.Msgs).To(Equal(uint64(1)))
				Expect(nfo.FirstSeq).To(Equal(uint64(1)))

				Expect(storage.RetryTaskByID(context.Background(), q, task.ID)).ToNot(HaveOccurred())

				nfo, err = storage.qStreams[q.Name].State()
				Expect(err).ToNot(HaveOccurred())
				Expect(nfo.Msgs).To(Equal(uint64(1)))
				Expect(nfo.FirstSeq).To(Equal(uint64(2)))

				task, err = storage.LoadTaskByID(task.ID)
				Expect(err).ToNot(HaveOccurred())

				Expect(task.State).To(Equal(TaskStateRetry))
			})
		})
	})

	Describe("EnqueueTask", func() {
		It("Save the task and handle save errors", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
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
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour, -1, false)
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

		It("Should not set dupe headers for retries", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour, -1, false)
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())
				task.State = TaskStateRetry

				err = storage.EnqueueTask(ctx, q, task)
				Expect(err).ToNot(HaveOccurred())

				msg, err := storage.qStreams[q.Name].ReadMessage(1)
				Expect(err).ToNot(HaveOccurred())
				Expect(msg.Header).To(HaveLen(0))
			})
		})

		It("Should enqueue task and job correctly", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour, -1, false)
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				Expect(task.Signature).To(HaveLen(0))
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
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.SaveTaskState(ctx, task, false)
				if err != nats.ErrNoResponders {
					Fail(fmt.Sprintf("Unexpected error: %v", err))
				}
			})
		})

		It("Should use the correct last subject sequence for new saves", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour, -1, false)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.SaveTaskState(ctx, task, false)
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
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour, -1, false)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.SaveTaskState(ctx, task, false)
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

				err = storage.SaveTaskState(ctx, task, false)
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
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour, -1, false)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.SaveTaskState(ctx, task, false)
				Expect(err).ToNot(HaveOccurred())

				// force the failure
				task.storageOptions.(*taskMeta).seq = 10

				err = storage.SaveTaskState(ctx, task, false)
				if !jsm.IsNatsError(err, 10071) {
					Fail(fmt.Sprintf("Unexpected error %v", err))
				}
			})
		})

		It("Should publish events", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())
				Expect(storage.PrepareTasks(true, 1, time.Hour, -1, false)).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				sub, err := nc.SubscribeSync(fmt.Sprintf(TaskStateChangeEventSubjectPattern, task.ID))
				Expect(err).ToNot(HaveOccurred())

				Expect(storage.SaveTaskState(ctx, task, true)).ToNot(HaveOccurred())

				event, err := sub.NextMsg(time.Second)
				Expect(err).ToNot(HaveOccurred())

				e, kind, err := ParseEventJSON(event.Data)
				Expect(err).ToNot(HaveOccurred())
				Expect(kind).To(Equal(TaskStateChangeEventType))
				Expect(e.(TaskStateChangeEvent).TaskID).To(Equal(task.ID))
			})
		})

		It("Should save the task correctly", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				err = storage.PrepareTasks(true, 1, time.Hour, -1, false)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", nil)
				Expect(err).ToNot(HaveOccurred())

				err = storage.SaveTaskState(ctx, task, false)
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
