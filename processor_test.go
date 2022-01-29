package asyncjobs

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Processor", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
		log.SetOutput(GinkgoWriter)
	})

	AfterEach(func() { cancel() })

	Describe("handler", func() {
		It("Should handle handler errors and update the task and NaK the item", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test")
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, defaultQueue.Name, task)).ToNot(HaveOccurred())

				wg := sync.WaitGroup{}
				wg.Add(1)

				router := NewTaskRouter()
				router.HandleFunc("ginkgo", func(ctx context.Context, t *Task) (interface{}, error) {
					wg.Done()
					return nil, fmt.Errorf("simulated failure")
				})

				// intercept the NaK
				sub, err := nc.SubscribeSync("$JS.ACK.CHORIA_AJ_Q_DEFAULT.WORKERS.>")
				Expect(err).ToNot(HaveOccurred())

				go client.Run(ctx, router)

				wg.Wait()
				time.Sleep(50 * time.Millisecond)

				task, err = client.LoadTaskByID(task.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(task.State).To(Equal(TaskStateRetry))
				Expect(task.Tries).To(Equal(1))

				msg, err := sub.NextMsg(time.Second)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(msg.Data)).To(MatchRegexp("-NAK {\"delay\":"))
			})
		})

		It("Should set task success and Ack the item", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test")
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, defaultQueue.Name, task)).ToNot(HaveOccurred())

				wg := sync.WaitGroup{}
				wg.Add(1)

				router := NewTaskRouter()
				router.HandleFunc("ginkgo", func(ctx context.Context, t *Task) (interface{}, error) {
					wg.Done()
					return "done", nil
				})

				// intercept the ack
				sub, err := nc.SubscribeSync("$JS.ACK.CHORIA_AJ_Q_DEFAULT.WORKERS.>")
				Expect(err).ToNot(HaveOccurred())

				go client.Run(ctx, router)

				wg.Wait()
				time.Sleep(50 * time.Millisecond)

				task, err = client.LoadTaskByID(task.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(task.State).To(Equal(TaskStateCompleted))
				Expect(task.Tries).To(Equal(1))

				msg, err := sub.NextMsg(time.Second)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(msg.Data)).To(Equal("+ACK"))
			})
		})
	})

	Describe("processMessage", func() {
		It("Should handle tasks that do not exist", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				proc, err := newProcessor(client)
				Expect(err).ToNot(HaveOccurred())

				<-proc.limiter

				err = proc.processMessage(ctx, client.opts.queues[defaultQueue.Name], &ProcessItem{JobID: "does.not.exist"})
				Expect(err).To(MatchError("task not found"))
			})
		})

		It("Should not process active tasks", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test")
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, defaultQueue.Name, task)).ToNot(HaveOccurred())
				Expect(client.setTaskActive(ctx, task)).ToNot(HaveOccurred())

				proc, err := newProcessor(client)
				Expect(err).ToNot(HaveOccurred())

				<-proc.limiter

				err = proc.processMessage(ctx, client.opts.queues[defaultQueue.Name], &ProcessItem{JobID: task.ID})
				Expect(err).To(MatchError("already active"))
			})
		})

		It("Should not process completed or expired tasks", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test")
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, defaultQueue.Name, task)).ToNot(HaveOccurred())

				proc, err := newProcessor(client)
				Expect(err).ToNot(HaveOccurred())

				<-proc.limiter
				task.State = TaskStateCompleted
				Expect(client.storage.SaveTaskState(ctx, task)).ToNot(HaveOccurred())
				err = proc.processMessage(ctx, client.opts.queues[defaultQueue.Name], &ProcessItem{JobID: task.ID})
				Expect(err).To(MatchError("already in state \"complete\""))

				<-proc.limiter
				task.State = TaskStateExpired
				Expect(client.storage.SaveTaskState(ctx, task)).ToNot(HaveOccurred())
				err = proc.processMessage(ctx, client.opts.queues[defaultQueue.Name], &ProcessItem{JobID: task.ID})
				Expect(err).To(MatchError("already in state \"expired\""))

			})
		})

		It("Should not process past deadline tasks, and it should set them to expired", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test", TaskDeadline(time.Now().Add(-1*time.Hour)))
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, defaultQueue.Name, task)).ToNot(HaveOccurred())

				proc, err := newProcessor(client)
				Expect(err).ToNot(HaveOccurred())

				<-proc.limiter
				err = proc.processMessage(ctx, client.opts.queues[defaultQueue.Name], &ProcessItem{JobID: task.ID})
				Expect(err).To(MatchError("past deadline"))

				task, err = client.LoadTaskByID(task.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(task.State).To(Equal(TaskStateExpired))
			})
		})

		It("Should support executing messages with deadlines in the future", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test", TaskDeadline(time.Now().Add(time.Hour)))
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, defaultQueue.Name, task)).ToNot(HaveOccurred())

				proc, err := newProcessor(client)
				Expect(err).ToNot(HaveOccurred())

				<-proc.limiter
				err = proc.processMessage(ctx, client.opts.queues[defaultQueue.Name], &ProcessItem{JobID: task.ID})
				Expect(err).To(MatchError("no router registered")) // hacky but this happens before any deadline checks etc
			})
		})

		It("Should set tasks as active and process them using the handler", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test")
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, defaultQueue.Name, task)).ToNot(HaveOccurred())

				wg := sync.WaitGroup{}
				wg.Add(1)

				router := NewTaskRouter()
				router.HandleFunc("ginkgo", func(ctx context.Context, task *Task) (interface{}, error) {
					t, err := client.LoadTaskByID(task.ID)
					Expect(err).ToNot(HaveOccurred())
					Expect(t.State).To(Equal(TaskStateActive))

					wg.Done()
					return "done", nil
				})

				go client.Run(ctx, router)

				wg.Wait()
				time.Sleep(50 * time.Millisecond)

				task, err = client.LoadTaskByID(task.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(task.State).To(Equal(TaskStateCompleted))
				Expect(task.Result).ToNot(BeNil())
				Expect(task.Result.CompletedAt).To(BeTemporally("~", time.Now(), 100*time.Millisecond))
				Expect(task.Result.Payload).To(Equal("done"))
				Expect(task.Tries).To(Equal(1))
			})
		})
	})

	Describe("newProcessor", func() {
		It("Should handle priorities correctly and set up a limiter", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				q10 := &Queue{Name: "10", Priority: 10}
				q2 := &Queue{Name: "2", Priority: 2}
				client, err := NewClient(NatsConn(nc), ClientConcurrency(5), WorkQueues(q10, q2))
				Expect(err).ToNot(HaveOccurred())

				proc, err := newProcessor(client)
				Expect(err).ToNot(HaveOccurred())

				Expect(proc.limiter).To(HaveCap(5))
				Expect(proc.limiter).To(HaveLen(5))
				Expect(proc.queues).To(HaveLen(12))

				entries := map[string]int{}
				for _, q := range proc.queues {
					_, ok := entries[q.Name]
					if !ok {
						entries[q.Name] = 0
					}
					entries[q.Name]++
				}

				Expect(entries["10"]).To(Equal(10))
				Expect(entries["2"]).To(Equal(2))
			})
		})
	})
})
