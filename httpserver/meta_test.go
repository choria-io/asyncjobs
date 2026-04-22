// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/choria-io/asyncjobs/httpserver/internal/gen"
)

var _ = Describe("Meta endpoints", func() {
	Describe("/healthz", func() {
		It("answers 200", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				srv := newTestServer(client)

				ts := httptest.NewServer(srv.Handler())
				defer ts.Close()

				resp, body := do(ts, http.MethodGet, "/healthz", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var h gen.HealthResponse
				Expect(json.Unmarshal([]byte(body), &h)).To(Succeed())
				Expect(h.Status).To(Equal(gen.Ok))
				Expect(h.Time.IsZero()).To(BeFalse())
			})
		})
	})

	Describe("/readyz", func() {
		It("answers 200 when storage is reachable", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				srv := newTestServer(client)

				ts := httptest.NewServer(srv.Handler())
				defer ts.Close()

				resp, body := do(ts, http.MethodGet, "/readyz", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var r gen.ReadinessResponse
				Expect(json.Unmarshal([]byte(body), &r)).To(Succeed())
				Expect(r.Ready).To(BeTrue())
				Expect(r.Checks.Tasks.Ok).To(BeTrue())
				Expect(r.Checks.Config.Ok).To(BeTrue())
			})
		})
	})

	Describe("/v1/openapi.json", func() {
		It("serves the embedded spec converted to JSON", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				srv := newTestServer(client)

				ts := httptest.NewServer(srv.Handler())
				defer ts.Close()

				resp, body := do(ts, http.MethodGet, "/v1/openapi.json", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var m map[string]any
				Expect(json.Unmarshal([]byte(body), &m)).To(Succeed())
				Expect(m).To(HaveKey("openapi"))
				Expect(m).To(HaveKey("paths"))
			})
		})
	})

	Describe("/v1/info", func() {
		It("reports auth=none when no mTLS is configured", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				srv := newTestServer(client)

				ts := httptest.NewServer(srv.Handler())
				defer ts.Close()

				resp, body := do(ts, http.MethodGet, "/v1/info", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var info gen.Info
				Expect(json.Unmarshal([]byte(body), &info)).To(Succeed())
				Expect(info.Version).ToNot(BeEmpty())
				Expect(info.Auth).To(Equal(gen.None))
				Expect(info.QueueCount).ToNot(BeNil())
				Expect(info.TaskCount).ToNot(BeNil())
			})
		})
	})

	Describe("/v1/retry-policies", func() {
		It("returns a non-empty policy list", func() {
			withJetStream(func(nc *nats.Conn) {
				client := newTestClient(nc)
				srv := newTestServer(client)

				ts := httptest.NewServer(srv.Handler())
				defer ts.Close()

				resp, body := do(ts, http.MethodGet, "/v1/retry-policies", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var out struct {
					Policies []gen.RetryPolicy `json:"policies"`
				}
				Expect(json.Unmarshal([]byte(body), &out)).To(Succeed())
				Expect(len(out.Policies)).To(BeNumerically(">", 0))
				for _, p := range out.Policies {
					Expect(p.Name).ToNot(BeEmpty())
					Expect(len(p.Intervals)).To(BeNumerically(">", 0))
				}
			})
		})
	})
})
