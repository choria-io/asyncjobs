package asyncjobs

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RetryPolicy", func() {
	Describe("Duration", func() {
		It("Should determine the correct interval with jitter", func() {
			b := RetryLinearOneMinute.Duration(10)
			d := RetryLinearOneMinute.Intervals[10]

			Expect(b).ToNot(Equal(d))
			Expect(b).To(BeNumerically(">", float64(d)*0.5))
			Expect(b).To(BeNumerically("<", float64(d)+float64(d)*0.5))
		})
	})

	Describe("RetryPolicyNames", func() {
		It("Should have the right names", func() {
			Expect(RetryPolicyNames()).To(Equal([]string{"10m", "1h", "1m", "default"}))
		})
	})

	Describe("RetryPolicyLookup", func() {
		It("Should find the right policy", func() {
			_, err := RetryPolicyLookup("missing")
			Expect(err).To(MatchError(ErrUnknownRetryPolicy))

			p, err := RetryPolicyLookup("1m")
			Expect(err).ToNot(HaveOccurred())
			Expect(p).To(Equal(RetryLinearOneMinute))
		})
	})

	Describe("IsRetryPolicyKnown", func() {
		It("Should report correct values", func() {
			Expect(IsRetryPolicyKnown("foo")).To(BeFalse())
			Expect(IsRetryPolicyKnown("default")).To(BeTrue())
			Expect(IsRetryPolicyKnown("1m")).To(BeTrue())
		})
	})
})
