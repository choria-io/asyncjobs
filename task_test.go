package asyncjobs

import (
	"context"
	"log"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Tasks", func() {
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

	Describe("NewTask", func() {
		It("Should create a valid task with options supplied", func() {
			p, err := NewTask("parent", nil)
			Expect(err).ToNot(HaveOccurred())

			deadline := time.Now().Add(time.Hour)
			payload := map[string]string{"hello": "world"}
			task, err := NewTask("test", payload,
				TaskDeadline(deadline),
				TaskDependsOnIDs("1", "2", "2", "1", "2"),
				TaskDependsOn(p, p),
				TaskRequiresDependencyResults(),
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(task.Deadline).To(Equal(&deadline))
			Expect(task.ID).ToNot(HaveLen(0))
			Expect(task.Type).To(Equal("test"))
			Expect(task.Payload).To(MatchJSON(`{"hello":"world"}`))
			Expect(task.CreatedAt).To(BeTemporally("~", time.Now(), 50*time.Millisecond))
			Expect(task.Dependencies).To(HaveLen(3))
			Expect(task.Dependencies).To(Equal([]string{"1", "2", p.ID}))
			Expect(task.State).To(Equal(TaskStateBlocked)) // because we have dependencies
			Expect(task.LoadDependencies).To(BeTrue())
			Expect(task.MaxTries).To(Equal(DefaultMaxTries))

			// without dependencies, should be new
			task, err = NewTask("test", payload, TaskDeadline(deadline), TaskMaxTries(10))
			Expect(err).ToNot(HaveOccurred())
			Expect(task.State).To(Equal(TaskStateNew))
			Expect(task.LoadDependencies).To(BeFalse())
			Expect(task.MaxTries).To(Equal(10))

			_, err = task.signatureMessage()
			Expect(err).To(MatchError(ErrTaskSignatureRequiresQueue))
			task.Queue = "x"
			msg, err := task.signatureMessage()
			Expect(err).ToNot(HaveOccurred())
			Expect(msg).To(HaveLen(102))
		})
	})

	Describe("NewTaskWithCustomID", func() {
		It("Should create a valid task with valid custom ID", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				CustomIDGenerator := func(t *Task) error {
					t.ID = "my-custom-id"
					return nil
				}

				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())
				err = storage.PrepareTasks(true, 1, time.Hour)
				Expect(err).ToNot(HaveOccurred())

				task, err := NewTask("test", nil, CustomIDGenerator)
				Expect(err).ToNot(HaveOccurred())
				Expect(task.ID).ToNot(HaveLen(0))
				Expect(task.ID).To(Equal("my-custom-id"))

				err = storage.EnqueueTask(ctx, q, task)
				Expect(err).ToNot(HaveOccurred())

				t, err := storage.LoadTaskByID(task.ID)
				Expect(err).ToNot(HaveOccurred())
				Expect(t.ID).To(Equal(task.ID))
				Expect(task.ID).To(Equal("my-custom-id"))
			})
		})

		It("Should not create a task with invalid custom ID", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				CustomIDGenerator := func(t *Task) error {
					t.ID = "my.invalid.custom.id"
					return nil
				}

				storage, err := newJetStreamStorage(nc, retryForTesting, &defaultLogger{})
				Expect(err).ToNot(HaveOccurred())

				q := testQueue()
				err = storage.PrepareQueue(q, 1, true)
				Expect(err).ToNot(HaveOccurred())
				err = storage.PrepareTasks(true, 1, time.Hour)
				Expect(err).ToNot(HaveOccurred())

				_, err = NewTask("test", nil, CustomIDGenerator)
				Expect(err).To(MatchError(ErrTaskIDInvalid))
			})
		})

	})
})
