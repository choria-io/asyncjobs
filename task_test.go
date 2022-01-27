package asyncjobs

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Tasks", func() {
	Describe("NewTask", func() {
		It("Should create a valid task with options supplied", func() {
			deadline := time.Now().Add(time.Hour)
			payload := map[string]string{"hello": "world"}
			task, err := NewTask("test", payload, TaskDeadline(deadline))
			Expect(err).ToNot(HaveOccurred())
			Expect(task.Deadline).To(Equal(&deadline))
			Expect(task.ID).ToNot(HaveLen(0))
			Expect(task.Type).To(Equal("test"))
			Expect(task.Payload).To(MatchJSON(`{"hello":"world"}`))
			Expect(task.CreatedAt).To(BeTemporally("~", time.Now(), 50*time.Millisecond))
			Expect(task.State).To(Equal(TaskStateNew))
		})
	})
})
