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
		It("Should handler termination errors and terminate the item", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test")
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, task)).ToNot(HaveOccurred())

				wg := sync.WaitGroup{}
				wg.Add(1)

				router := NewTaskRouter()
				router.HandleFunc("ginkgo", func(_ context.Context, _ Logger, t *Task) (any, error) {
					wg.Done()
					return nil, fmt.Errorf("simulated failure: %w", ErrTerminateTask)
				})

				// intercept the ack
				sub, err := nc.SubscribeSync("$JS.ACK.CHORIA_AJ_Q_DEFAULT.WORKERS.>")
				Expect(err).ToNot(HaveOccurred())

				go client.Run(ctx, router)

				wg.Wait()
				time.Sleep(50 * time.Millisecond)

				task, err = client.LoadTaskByID(task.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(task.State).To(Equal(TaskStateTerminated))
				Expect(task.Tries).To(Equal(1))

				msg, err := sub.NextMsg(time.Second)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(msg.Data)).To(Equal("+TERM"))

				nfo, err := client.StorageAdmin().QueueInfo(client.opts.queue.Name)
				Expect(err).ToNot(HaveOccurred())
				Expect(nfo.Stream.State.Msgs).To(Equal(uint64(0)))
			})
		})

		It("Should handle handler errors and update the task and NaK the item", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test")
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, task)).ToNot(HaveOccurred())

				wg := sync.WaitGroup{}
				wg.Add(1)

				router := NewTaskRouter()
				router.HandleFunc("ginkgo", func(_ context.Context, _ Logger, t *Task) (any, error) {
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
				Expect(client.EnqueueTask(ctx, task)).ToNot(HaveOccurred())

				wg := sync.WaitGroup{}
				wg.Add(1)

				router := NewTaskRouter()
				router.HandleFunc("ginkgo", func(_ context.Context, _ Logger, t *Task) (any, error) {
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
		It("Should handle tasks that do not exist by terminating the item", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				proc, err := newProcessor(client)
				Expect(err).ToNot(HaveOccurred())

				<-proc.limiter

				err = proc.processMessage(ctx, &ProcessItem{JobID: "does.not.exist"})
				Expect(err).ToNot(HaveOccurred())

				nfo, err := client.StorageAdmin().QueueInfo("DEFAULT")
				Expect(err).ToNot(HaveOccurred())
				Expect(nfo.Stream.State.Msgs).To(Equal(uint64(0)))
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
				Expect(client.EnqueueTask(ctx, task)).ToNot(HaveOccurred())
				Expect(client.setTaskActive(ctx, task)).ToNot(HaveOccurred())

				proc, err := newProcessor(client)
				Expect(err).ToNot(HaveOccurred())

				<-proc.limiter

				err = proc.processMessage(ctx, &ProcessItem{JobID: task.ID})
				Expect(err).To(Equal(ErrTaskAlreadyActive))
			})
		})

		It("Should detect stale active tasks", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				q := newDefaultQueue()
				q.MaxRunTime = 100 * time.Millisecond

				client, err := NewClient(NatsConn(nc), WorkQueue(q))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test")
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, task)).ToNot(HaveOccurred())
				Expect(client.setTaskActive(ctx, task)).ToNot(HaveOccurred())

				proc, err := newProcessor(client)
				Expect(err).ToNot(HaveOccurred())

				// sleep till past queue max age
				time.Sleep(200 * time.Millisecond)

				<-proc.limiter

				Expect(proc.processMessage(ctx, &ProcessItem{JobID: task.ID})).ToNot(HaveOccurred())
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
				Expect(client.EnqueueTask(ctx, task)).ToNot(HaveOccurred())

				proc, err := newProcessor(client)
				Expect(err).ToNot(HaveOccurred())

				<-proc.limiter
				task.State = TaskStateCompleted
				Expect(client.storage.SaveTaskState(ctx, task, false)).ToNot(HaveOccurred())
				err = proc.processMessage(ctx, &ProcessItem{JobID: task.ID})
				Expect(err).To(MatchError(ErrTaskAlreadyInState))

				<-proc.limiter
				task.State = TaskStateExpired
				Expect(client.storage.SaveTaskState(ctx, task, false)).ToNot(HaveOccurred())
				err = proc.processMessage(ctx, &ProcessItem{JobID: task.ID})
				Expect(err).To(MatchError(ErrTaskAlreadyInState))

			})
		})

		It("Should not process past max tries tasks, and it should set them to expired", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test", TaskMaxTries(1))
				task.Tries = 2
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, task)).ToNot(HaveOccurred())

				proc, err := newProcessor(client)
				Expect(err).ToNot(HaveOccurred())

				<-proc.limiter
				err = proc.processMessage(ctx, &ProcessItem{JobID: task.ID})
				Expect(err).To(MatchError(ErrTaskExceedsMaxTries))

				task, err = client.LoadTaskByID(task.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(task.State).To(Equal(TaskStateExpired))
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
				Expect(client.EnqueueTask(ctx, task)).ToNot(HaveOccurred())

				proc, err := newProcessor(client)
				Expect(err).ToNot(HaveOccurred())

				<-proc.limiter
				err = proc.processMessage(ctx, &ProcessItem{JobID: task.ID})
				Expect(err).To(MatchError(ErrTaskPastDeadline))

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
				Expect(client.EnqueueTask(ctx, task)).ToNot(HaveOccurred())

				proc, err := newProcessor(client)
				Expect(err).ToNot(HaveOccurred())

				<-proc.limiter
				Expect(proc.processMessage(ctx, &ProcessItem{JobID: task.ID})).ToNot(HaveOccurred())
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
				Expect(client.EnqueueTask(ctx, task)).ToNot(HaveOccurred())

				wg := sync.WaitGroup{}
				wg.Add(1)

				router := NewTaskRouter()
				router.HandleFunc("ginkgo", func(ctx context.Context, log Logger, task *Task) (any, error) {
					// these will panic as its in a different routine, but they are supposed to pass so thats fine
					t, err := client.LoadTaskByID(task.ID)
					Expect(err).ToNot(HaveOccurred())
					Expect(t.State).To(Equal(TaskStateActive))
					Expect(log).ToNot(BeNil())

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

	Describe("Dependencies", func() {
		BeforeEach(func() {
			defaultBlockedNakTime = 10 * time.Millisecond
		})

		AfterEach(func() {
			defaultBlockedNakTime = 5 * time.Second
		})

		It("Should fail for unresolvable dependencies", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				parent, err := NewTask("ginkgo", "parent")
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test", TaskDependsOn(parent))
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, task)).ToNot(HaveOccurred())

				router := NewTaskRouter()
				go client.Run(ctx, router)

				time.Sleep(50 * time.Millisecond)

				task, err = client.LoadTaskByID(task.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(task.State).To(Equal(TaskStateUnreachable))
				Expect(task.Tries).To(Equal(0))
			})
		})

		It("Should fail for failed dependencies", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				p1, err := NewTask("ginkgo", "parent")
				Expect(err).ToNot(HaveOccurred())
				p1.State = TaskStateExpired
				Expect(client.storage.SaveTaskState(ctx, p1, false)).ToNot(HaveOccurred())

				p2, err := NewTask("ginkgo", "parent")
				Expect(err).ToNot(HaveOccurred())
				p2.State = TaskStateCompleted
				Expect(client.storage.SaveTaskState(ctx, p2, false)).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test", TaskDependsOn(p1, p2))
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, task)).ToNot(HaveOccurred())

				router := NewTaskRouter()
				go client.Run(ctx, router)

				time.Sleep(50 * time.Millisecond)
				task, err = client.LoadTaskByID(task.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(task.State).To(Equal(TaskStateUnreachable))
				Expect(task.Tries).To(Equal(0))
			})
		})

		It("Should run tasks in order", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				Expect(client.setupStreams()).ToNot(HaveOccurred())
				Expect(client.setupQueues()).ToNot(HaveOccurred())

				p1, err := NewTask("ginkgo", "parent")
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, p1)).ToNot(HaveOccurred())

				p2, err := NewTask("ginkgo", "parent")
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, p2)).ToNot(HaveOccurred())

				task, err := NewTask("ginkgo", "test", TaskDependsOn(p1, p2), TaskRequiresDependencyResults())
				Expect(err).ToNot(HaveOccurred())
				Expect(client.EnqueueTask(ctx, task)).ToNot(HaveOccurred())

				wg := sync.WaitGroup{}
				wg.Add(3)
				mu := sync.Mutex{}
				var runs []string

				router := NewTaskRouter()
				router.HandleFunc("ginkgo", func(ctx context.Context, log Logger, task *Task) (any, error) {
					mu.Lock()
					defer mu.Unlock()
					defer wg.Done()

					time.Sleep(500 * time.Millisecond)

					runs = append(runs, task.ID)

					if task.HasDependencies() {
						Expect(task.DependencyResults).To(HaveLen(2))
					}

					return task.ID, nil
				})

				go client.Run(ctx, router)

				wg.Wait()
				time.Sleep(50 * time.Millisecond)

				task, err = client.LoadTaskByID(task.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(task.State).To(Equal(TaskStateCompleted))
				Expect(runs).To(HaveLen(3))
				Expect(runs[2]).To(Equal(task.ID))
			})
		})
	})

	Describe("newProcessor", func() {
		It("Should et up a limiter", func() {
			withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
				client, err := NewClient(NatsConn(nc), ClientConcurrency(5))
				Expect(err).ToNot(HaveOccurred())

				proc, err := newProcessor(client)
				Expect(err).ToNot(HaveOccurred())

				Expect(proc.limiter).To(HaveCap(5))
				Expect(proc.limiter).To(HaveLen(5))
			})
		})
	})
})
