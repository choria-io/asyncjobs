package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	magicWord = "Processed"
)

func TestAsyncJobs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AsyncJobs")
}

func withHTTPServer(cb func(resp *string)) {
	d, err := ioutil.TempDir("", "jstest")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(d)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(magicWord))
	}))
	defer server.Close()

	var out = magicWord
	cb(&out)
}

var _ = Describe("API Client", func() {
	BeforeEach(func() {
		log.SetOutput(GinkgoWriter)
	})

	It("Should", func() {
		withHTTPServer(func(resp *string) {
			Expect(*resp).To(Equal(magicWord))
		})
	})
})
