// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/choria-io/asyncjobs"
	"github.com/choria-io/asyncjobs/httpserver/internal/gen"
)

func newSchedulesTestEnv(nc *nats.Conn) (*httptest.Server, *asyncjobs.Client, func()) {
	client := newTestClient(nc)
	srv := newTestServer(client)
	ts := httptest.NewServer(srv.Handler())
	cleanup := func() {
		ts.Close()
	}
	return ts, client, cleanup
}

var _ = Describe("Schedules", func() {
	Describe("POST /v1/schedules", func() {
		It("creates a schedule with a deadline_offset that round-trips", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newSchedulesTestEnv(nc)
				defer cleanup()

				body := `{"name":"nightly","schedule":"@every 1h","queue":"DEFAULT","task_type":"email","deadline_offset":"5m"}`
				resp, raw := do(ts, http.MethodPost, "/v1/schedules", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var out gen.Schedule
				Expect(json.Unmarshal([]byte(raw), &out)).To(Succeed())
				Expect(out.Name).To(Equal("nightly"))
				Expect(out.Queue).To(Equal("DEFAULT"))
				Expect(out.Schedule).To(Equal("@every 1h"))
				Expect(out.TaskType).To(Equal("email"))
				Expect(out.Deadline).ToNot(BeNil())
				Expect(*out.Deadline).To(Equal(int64(5 * time.Minute)))
			})
		})

		It("returns 409 when the schedule already exists", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newSchedulesTestEnv(nc)
				defer cleanup()

				body := `{"name":"dupe","schedule":"@every 1h","queue":"DEFAULT","task_type":"email"}`
				first, _ := do(ts, http.MethodPost, "/v1/schedules", strings.NewReader(body))
				Expect(first.StatusCode).To(Equal(http.StatusCreated))

				resp, raw := do(ts, http.MethodPost, "/v1/schedules", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusConflict))
				Expect(raw).To(ContainSubstring(`"reason":"schedule_already_exists"`))
			})
		})

		It("maps invalid cron expressions to schedule_invalid", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newSchedulesTestEnv(nc)
				defer cleanup()

				body := `{"name":"badcron","schedule":"not a cron","queue":"DEFAULT","task_type":"email"}`
				resp, raw := do(ts, http.MethodPost, "/v1/schedules", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(raw).To(ContainSubstring(`"reason":"schedule_invalid"`))
			})
		})

		It("rejects malformed deadline_offset", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newSchedulesTestEnv(nc)
				defer cleanup()

				body := `{"name":"bogus","schedule":"@every 1h","queue":"DEFAULT","task_type":"email","deadline_offset":"wrong"}`
				resp, raw := do(ts, http.MethodPost, "/v1/schedules", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(raw).To(ContainSubstring(`"reason":"invalid_duration"`))
				Expect(raw).To(ContainSubstring(`"field":"deadline_offset"`))
			})
		})

		It("rejects setting both payload and payload_base64", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newSchedulesTestEnv(nc)
				defer cleanup()

				body := `{"name":"both","schedule":"@every 1h","queue":"DEFAULT","task_type":"email","payload":1,"payload_base64":"aGVsbG8="}`
				resp, raw := do(ts, http.MethodPost, "/v1/schedules", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(raw).To(ContainSubstring(`"reason":"payload_both_set"`))
			})
		})
	})

	Describe("GET /v1/schedules", func() {
		It("returns the stored schedules", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newSchedulesTestEnv(nc)
				defer cleanup()

				do(ts, http.MethodPost, "/v1/schedules", strings.NewReader(`{"name":"s1","schedule":"@every 1h","queue":"DEFAULT","task_type":"t1"}`))
				do(ts, http.MethodPost, "/v1/schedules", strings.NewReader(`{"name":"s2","schedule":"@every 2h","queue":"DEFAULT","task_type":"t2"}`))

				resp, raw := do(ts, http.MethodGet, "/v1/schedules", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var out struct {
					Schedules []gen.Schedule `json:"schedules"`
				}
				Expect(json.Unmarshal([]byte(raw), &out)).To(Succeed())
				names := map[string]bool{}
				for _, st := range out.Schedules {
					names[st.Name] = true
				}
				Expect(names).To(HaveKey("s1"))
				Expect(names).To(HaveKey("s2"))
			})
		})
	})

	Describe("GET /v1/schedules/{name}", func() {
		It("returns 200 for a known schedule", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newSchedulesTestEnv(nc)
				defer cleanup()

				do(ts, http.MethodPost, "/v1/schedules", strings.NewReader(`{"name":"show","schedule":"@every 1h","queue":"DEFAULT","task_type":"t"}`))

				resp, raw := do(ts, http.MethodGet, "/v1/schedules/show", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var out gen.Schedule
				Expect(json.Unmarshal([]byte(raw), &out)).To(Succeed())
				Expect(out.Name).To(Equal("show"))
			})
		})

		It("returns 404 for an unknown schedule", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newSchedulesTestEnv(nc)
				defer cleanup()

				resp, raw := do(ts, http.MethodGet, "/v1/schedules/missing", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				Expect(raw).To(ContainSubstring(`"reason":"schedule_not_found"`))
			})
		})
	})

	Describe("DELETE /v1/schedules/{name}", func() {
		It("removes the schedule and returns 204", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, cleanup := newSchedulesTestEnv(nc)
				defer cleanup()

				do(ts, http.MethodPost, "/v1/schedules", strings.NewReader(`{"name":"rm","schedule":"@every 1h","queue":"DEFAULT","task_type":"t"}`))

				resp, _ := do(ts, http.MethodDelete, "/v1/schedules/rm", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

				follow, raw := do(ts, http.MethodGet, "/v1/schedules/rm", nil)
				Expect(follow.StatusCode).To(Equal(http.StatusNotFound))
				Expect(raw).To(ContainSubstring(`"reason":"schedule_not_found"`))
			})
		})
	})
})
