// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/choria-io/asyncjobs"
	"github.com/choria-io/asyncjobs/httpserver/internal/gen"
)

// newTasksTestEnv stands up a client + server and returns the httptest.Server
// plus the underlying asyncjobs client and server. Callers close the returned
// server themselves.
func newTasksTestEnv(nc *nats.Conn) (*httptest.Server, *asyncjobs.Client, *Server, func()) {
	client := newTestClient(nc)
	srv := newTestServer(client)
	ts := httptest.NewServer(srv.Handler())
	cleanup := func() {
		ts.Close()
	}
	return ts, client, srv, cleanup
}

var _ = Describe("Tasks", func() {
	Describe("POST /v1/tasks", func() {
		It("creates and enqueues a task with a JSON payload", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				body := `{"type":"email","payload":{"to":"x@y"}}`
				resp, raw := do(ts, http.MethodPost, "/v1/tasks", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var task gen.Task
				Expect(json.Unmarshal([]byte(raw), &task)).To(Succeed())
				Expect(task.Id).ToNot(BeEmpty())
				Expect(task.Type).To(Equal("email"))
				Expect(task.State).To(Equal(gen.New))
				Expect(resp.Header.Get("Location")).To(Equal("/v1/tasks/" + task.Id))
				Expect(string(task.Payload)).To(Equal(`{"to":"x@y"}`))
			})
		})

		It("accepts payload_base64 verbatim", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				// "hello" base64
				body := `{"type":"raw","payload_base64":"aGVsbG8="}`
				resp, raw := do(ts, http.MethodPost, "/v1/tasks", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var task gen.Task
				Expect(json.Unmarshal([]byte(raw), &task)).To(Succeed())
				Expect(string(task.Payload)).To(Equal("hello"))
			})
		})

		It("rejects both payload and payload_base64 being set", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				body := `{"type":"t","payload":1,"payload_base64":"aGVsbG8="}`
				resp, raw := do(ts, http.MethodPost, "/v1/tasks", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(raw).To(ContainSubstring(`"reason":"payload_both_set"`))
				Expect(raw).To(ContainSubstring(`"field":"payload"`))
			})
		})

		It("rejects invalid task types", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				body := `{"type":"bad type!"}`
				resp, raw := do(ts, http.MethodPost, "/v1/tasks", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(raw).To(ContainSubstring(`"reason":"task_type_invalid"`))
			})
		})

		It("accepts tasks with a future deadline", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
				body := fmt.Sprintf(`{"type":"email","deadline":%q}`, future)
				resp, raw := do(ts, http.MethodPost, "/v1/tasks", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var t gen.Task
				Expect(json.Unmarshal([]byte(raw), &t)).To(Succeed())
				Expect(t.Deadline).ToNot(BeNil())
			})
		})

		It("reuses a task for a repeated Idempotency-Key", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/tasks", strings.NewReader(`{"type":"email"}`))
				req.Header.Set("Idempotency-Key", "k-1")
				resp, err := ts.Client().Do(req)
				Expect(err).ToNot(HaveOccurred())
				b1, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				var first gen.Task
				Expect(json.Unmarshal(b1, &first)).To(Succeed())

				req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/tasks", strings.NewReader(`{"type":"email"}`))
				req2.Header.Set("Idempotency-Key", "k-1")
				resp2, err := ts.Client().Do(req2)
				Expect(err).ToNot(HaveOccurred())
				b2, _ := io.ReadAll(resp2.Body)
				resp2.Body.Close()
				Expect(resp2.StatusCode).To(Equal(http.StatusOK))

				var second gen.Task
				Expect(json.Unmarshal(b2, &second)).To(Succeed())
				Expect(second.Id).To(Equal(first.Id))
			})
		})
	})

	Describe("GET /v1/tasks/{id}", func() {
		It("returns 200 when the task exists", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, client, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				t := enqueueTestTask(client, "email", map[string]string{"to": "x"})

				resp, body := do(ts, http.MethodGet, "/v1/tasks/"+t.ID, nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var got gen.Task
				Expect(json.Unmarshal([]byte(body), &got)).To(Succeed())
				Expect(got.Id).To(Equal(t.ID))
			})
		})

		It("returns 404 when the task does not exist", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				resp, body := do(ts, http.MethodGet, "/v1/tasks/does-not-exist", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				Expect(body).To(ContainSubstring(`"reason":"task_not_found"`))
			})
		})
	})

	Describe("DELETE /v1/tasks/{id}", func() {
		It("removes an existing task", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, client, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				t := enqueueTestTask(client, "email", nil)

				resp, _ := do(ts, http.MethodDelete, "/v1/tasks/"+t.ID, nil)
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

				_, err := client.LoadTaskByID(t.ID)
				Expect(err).To(MatchError(asyncjobs.ErrTaskNotFound))
			})
		})

		It("reports 404 for unknown ids", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				resp, body := do(ts, http.MethodDelete, "/v1/tasks/never-there", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
				Expect(body).To(ContainSubstring(`"reason":"task_not_found"`))
			})
		})
	})

	Describe("POST /v1/tasks/{id}/retry", func() {
		It("re-enqueues the task and returns the refreshed body", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, client, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				t := enqueueTestTask(client, "email", nil)

				resp, body := do(ts, http.MethodPost, "/v1/tasks/"+t.ID+"/retry", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var out gen.Task
				Expect(json.Unmarshal([]byte(body), &out)).To(Succeed())
				Expect(out.Id).To(Equal(t.ID))
			})
		})
	})

	Describe("POST /v1/tasks/retry", func() {
		It("returns per-item results with mixed outcomes", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, client, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				good := enqueueTestTask(client, "email", nil)

				body := fmt.Sprintf(`{"ids":[%q,"missing-id"]}`, good.ID)
				resp, raw := do(ts, http.MethodPost, "/v1/tasks/retry", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var out gen.BulkRetryResponse
				Expect(json.Unmarshal([]byte(raw), &out)).To(Succeed())
				Expect(out.Results).To(HaveLen(2))

				statuses := map[string]gen.BulkRetryResultStatus{}
				for _, r := range out.Results {
					statuses[r.Id] = r.Status
				}
				Expect(statuses[good.ID]).To(Equal(gen.BulkRetryResultStatusOk))
				Expect(statuses["missing-id"]).To(Equal(gen.BulkRetryResultStatusNotFound))
			})
		})

		It("rejects bulk requests that exceed the cap", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				ids := make([]string, bulkRetryMax+1)
				for i := range ids {
					ids[i] = fmt.Sprintf("id-%d", i)
				}
				b, _ := json.Marshal(map[string]any{"ids": ids})

				resp, raw := do(ts, http.MethodPost, "/v1/tasks/retry", bytes.NewReader(b))
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(raw).To(ContainSubstring(`"reason":"ids_too_many"`))
			})
		})

		It("rejects empty id lists", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				resp, raw := do(ts, http.MethodPost, "/v1/tasks/retry", strings.NewReader(`{"ids":[]}`))
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(raw).To(ContainSubstring(`"reason":"ids_required"`))
			})
		})
	})

	Describe("GET /v1/tasks", func() {
		It("returns an empty list when the store has no tasks", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, _, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				resp, body := do(ts, http.MethodGet, "/v1/tasks", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var out gen.TaskListResponse
				Expect(json.Unmarshal([]byte(body), &out)).To(Succeed())
				Expect(out.Count).To(Equal(0))
				Expect(out.Tasks).To(BeEmpty())
			})
		})

		It("filters by type", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, client, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				enqueueTestTask(client, "alpha", nil)
				enqueueTestTask(client, "alpha", nil)
				enqueueTestTask(client, "beta", nil)

				resp, body := do(ts, http.MethodGet, "/v1/tasks?type=alpha", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var out gen.TaskListResponse
				Expect(json.Unmarshal([]byte(body), &out)).To(Succeed())
				Expect(out.Count).To(Equal(2))
				for _, t := range out.Tasks {
					Expect(t.Type).To(Equal("alpha"))
				}
			})
		})

		It("streams newline-delimited JSON", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, client, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				enqueueTestTask(client, "n1", nil)
				enqueueTestTask(client, "n2", nil)

				req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/tasks?stream=ndjson", nil)

				resp, err := ts.Client().Do(req)
				Expect(err).ToNot(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(resp.Header.Get("Content-Type")).To(Equal(contentTypeNDJSON))

				buf := &bytes.Buffer{}
				_, err = io.Copy(buf, resp.Body)
				Expect(err).ToNot(HaveOccurred())

				scanner := bufio.NewScanner(buf)
				types := map[string]bool{}
				for scanner.Scan() {
					var t gen.Task
					Expect(json.Unmarshal(scanner.Bytes(), &t)).To(Succeed())
					types[t.Type] = true
				}
				Expect(types).To(HaveKey("n1"))
				Expect(types).To(HaveKey("n2"))
			})
		})

		It("respects context cancellation on an ndjson stream", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, client, _, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				enqueueTestTask(client, "cc", nil)

				ctx, cancel := context.WithCancel(context.Background())
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/tasks?stream=ndjson", nil)

				resp, err := ts.Client().Do(req)
				Expect(err).ToNot(HaveOccurred())
				// Drain the first line (if any) then cancel.
				go func() {
					time.Sleep(100 * time.Millisecond)
					cancel()
				}()
				_, _ = io.ReadAll(resp.Body)
				resp.Body.Close()
			})
		})

		It("returns 429 when a concurrent list is already running", func() {
			withJetStream(func(nc *nats.Conn) {
				ts, client, srv, cleanup := newTasksTestEnv(nc)
				defer cleanup()

				enqueueTestTask(client, "rl", nil)

				// Simulate an in-flight list by acquiring the semaphore directly.
				Expect(srv.listSem.tryAcquire()).To(BeTrue())
				defer srv.listSem.release()

				resp, body := do(ts, http.MethodGet, "/v1/tasks", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusTooManyRequests))
				Expect(body).To(ContainSubstring(`"code":"rate_limited"`))
			})
		})
	})

	Describe("idempotencyStore", func() {
		It("expires entries once past the TTL", func() {
			s := newIdempotencyStore()
			s.store("k", "id-1")
			// Force expiry without sleeping for 10 minutes.
			s.entries["k"] = idempotencyEntry{taskID: "id-1", expires: time.Now().Add(-time.Second)}

			_, ok := s.lookup("k")
			Expect(ok).To(BeFalse())
		})

		It("is safe under concurrent access", func() {
			s := newIdempotencyStore()
			var wg sync.WaitGroup
			for i := 0; i < 20; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					s.store(fmt.Sprintf("k-%d", i), fmt.Sprintf("id-%d", i))
					s.lookup(fmt.Sprintf("k-%d", i))
				}(i)
			}
			wg.Wait()
		})
	})
})

// enqueueTestTask builds and enqueues a task through the library client so
// downstream assertions operate on real storage state.
func enqueueTestTask(client *asyncjobs.Client, taskType string, payload any) *asyncjobs.Task {
	task, err := asyncjobs.NewTask(taskType, payload)
	Expect(err).ToNot(HaveOccurred())
	Expect(client.EnqueueTask(context.Background(), task)).To(Succeed())
	return task
}
