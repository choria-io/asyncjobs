package asyncjobs

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RetryPolicy", func() {
	It("Should determine the correct interval with jitter", func() {
		b := RetryLinearOneMinute.Duration(10)
		d := RetryLinearOneMinute.Intervals[10]

		fmt.Printf("\nduration: %v backoff: %v\n", RetryLinearOneMinute.Intervals[10], b)
		Expect(b).ToNot(Equal(d))
		Expect(b).To(BeNumerically(">", float64(d)*0.5))
		Expect(b).To(BeNumerically("<", float64(d)+float64(d)*0.5))
	})
})
