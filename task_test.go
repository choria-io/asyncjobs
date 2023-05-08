package asyncjobs

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Tasks", func() {
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
})
