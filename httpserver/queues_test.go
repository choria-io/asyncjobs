// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/choria-io/asyncjobs"
	"github.com/choria-io/asyncjobs/httpserver/internal/gen"
)

// newQueuesTestEnv stands up a server and httptest.Server for the queues
// tests.
func newQueuesTestEnv(nc *nats.Conn) (*httptest.Server, *asyncjobs.Client, func()) {
	client := newTestClient(nc)
	srv := newTestServer(client)
	ts := httptest.NewServer(srv.Handler())
	cleanup := func() {
		ts.Close()
	}
	return ts, client, cleanup
}

var _ = Describe("Queues", func() {
	Describe("POST /v1/queues", func() {
		It("creates a queue with duration strings and returns nanosecond durations", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newQueuesTestEnv(nc)
				defer cleanup()

				body := `{"name":"alpha","max_runtime":"30s","max_concurrent":42}`
				resp, raw := do(ts, http.MethodPost, "/v1/queues", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var out gen.QueueInfo
				Expect(json.Unmarshal([]byte(raw), &out)).To(Succeed())
				Expect(out.Name).To(Equal("alpha"))
				Expect(out.MaxRuntime).ToNot(BeNil())
				Expect(*out.MaxRuntime).To(Equal(int64(30_000_000_000)))
				Expect(out.MaxConcurrent).ToNot(BeNil())
				Expect(*out.MaxConcurrent).To(Equal(42))
			})
		})

		It("rejects DEFAULT unless explicitly allowed", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newQueuesTestEnv(nc)
				defer cleanup()

				body := `{"name":"DEFAULT"}`
				resp, raw := do(ts, http.MethodPost, "/v1/queues", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(raw).To(ContainSubstring(`"reason":"default_queue_disallowed"`))
			})
		})

		It("returns 409 when the queue already exists", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newQueuesTestEnv(nc)
				defer cleanup()

				body := `{"name":"dupe"}`
				first, _ := do(ts, http.MethodPost, "/v1/queues", strings.NewReader(body))
				Expect(first.StatusCode).To(Equal(http.StatusCreated))

				resp, raw := do(ts, http.MethodPost, "/v1/queues", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
				Expect(raw).To(ContainSubstring(`"reason":"queue_already_exists"`))
			})
		})

		It("rejects malformed duration fields", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newQueuesTestEnv(nc)
				defer cleanup()

				body := `{"name":"bogus","max_age":"not-a-duration"}`
				resp, raw := do(ts, http.MethodPost, "/v1/queues", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(raw).To(ContainSubstring(`"reason":"invalid_duration"`))
				Expect(raw).To(ContainSubstring(`"field":"max_age"`))
			})
		})
	})

	Describe("GET /v1/queues", func() {
		It("lists queues without exposing stream or consumer detail", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newQueuesTestEnv(nc)
				defer cleanup()

				post, _ := do(ts, http.MethodPost, "/v1/queues", strings.NewReader(`{"name":"listq"}`))
				Expect(post.StatusCode).To(Equal(http.StatusCreated))

				resp, raw := do(ts, http.MethodGet, "/v1/queues", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(raw).ToNot(ContainSubstring("stream_info"))
				Expect(raw).ToNot(ContainSubstring("consumer_info"))

				var out struct {
					Queues []gen.QueueInfo `json:"queues"`
				}
				Expect(json.Unmarshal([]byte(raw), &out)).To(Succeed())
				names := map[string]bool{}
				for _, q := range out.Queues {
					names[q.Name] = true
				}
				Expect(names).To(HaveKey("listq"))
			})
		})
	})

	Describe("GET /v1/queues/{name}", func() {
		It("never returns stream_info or consumer_info", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newQueuesTestEnv(nc)
				defer cleanup()

				do(ts, http.MethodPost, "/v1/queues", strings.NewReader(`{"name":"scrub"}`))

				resp, raw := do(ts, http.MethodGet, "/v1/queues/scrub", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(raw).ToNot(ContainSubstring("stream_info"))
				Expect(raw).ToNot(ContainSubstring("consumer_info"))
			})
		})

		It("returns 404 for an unknown queue", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newQueuesTestEnv(nc)
				defer cleanup()

				resp, raw := do(ts, http.MethodGet, "/v1/queues/missing", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				Expect(raw).To(ContainSubstring(`"reason":"queue_not_found"`))
			})
		})
	})

	Describe("POST /v1/queues/{name}/purge", func() {
		It("returns 202 after a successful purge", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newQueuesTestEnv(nc)
				defer cleanup()

				do(ts, http.MethodPost, "/v1/queues", strings.NewReader(`{"name":"purgeme"}`))
				resp, _ := do(ts, http.MethodPost, "/v1/queues/purgeme/purge", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			})
		})

		It("returns 404 for unknown queues", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newQueuesTestEnv(nc)
				defer cleanup()

				resp, raw := do(ts, http.MethodPost, "/v1/queues/never/purge", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				Expect(raw).To(ContainSubstring(`"reason":"queue_not_found"`))
			})
		})
	})

	Describe("DELETE /v1/queues/{name}", func() {
		It("removes the queue and returns 204", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newQueuesTestEnv(nc)
				defer cleanup()

				do(ts, http.MethodPost, "/v1/queues", strings.NewReader(`{"name":"gone"}`))
				resp, _ := do(ts, http.MethodDelete, "/v1/queues/gone", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

				follow, raw := do(ts, http.MethodGet, "/v1/queues/gone", nil)
				Expect(follow.StatusCode).To(Equal(http.StatusNotFound))
				Expect(raw).To(ContainSubstring(`"reason":"queue_not_found"`))
			})
		})
	})
})
