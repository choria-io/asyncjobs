// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

	"github.com/choria-io/asyncjobs"
	"github.com/choria-io/asyncjobs/httpserver"
)

func TestAjc(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ajc")
}

// pickFreePort returns a host:port bound to an ephemeral local port. The
// listener is closed before returning, so the port may be racy under
// extreme load — sufficient for an integration test on an idle CI host.
func pickFreePort() string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).ToNot(HaveOccurred())
	addr := l.Addr().String()
	Expect(l.Close()).To(Succeed())
	return addr
}

func withJetStream(cb func(nc *nats.Conn)) {
	d, err := os.MkdirTemp("", "ajcsrv")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(d)

	opts := &server.Options{
		JetStream: true,
		StoreDir:  d,
		Port:      -1,
		Host:      "localhost",
	}
	s, err := server.NewServer(opts)
	Expect(err).ToNot(HaveOccurred())
	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		Fail("nats server did not start")
	}
	defer func() {
		s.Shutdown()
		s.WaitForShutdown()
	}()

	nc, err := nats.Connect(s.ClientURL(), nats.UseOldRequestStyle())
	Expect(err).ToNot(HaveOccurred())
	defer nc.Close()

	cb(nc)
}

// runServer boots an HTTP server using the same wiring exposed by ajc server.
// It returns the bind address and a cancel func that shuts the server down.
func runServer(nc *nats.Conn, c *serverCommand) (string, func()) {
	logger := logrus.New()
	logger.SetOutput(GinkgoWriter)
	log = logrus.NewEntry(logger)

	c.bind = pickFreePort()

	cl, err := asyncjobs.NewClient(append([]asyncjobs.ClientOpt{asyncjobs.NatsConn(nc)}, c.clientOptions()...)...)
	Expect(err).ToNot(HaveOccurred())

	srv, err := httpserver.NewServer(cl, c.serverOptions()...)
	Expect(err).ToNot(HaveOccurred())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- srv.Run(ctx)
	}()

	Eventually(func() error {
		conn, err := net.DialTimeout("tcp", c.bind, time.Second)
		if err != nil {
			return err
		}
		conn.Close()
		return nil
	}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

	return c.bind, func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			Fail("server did not shut down")
		}
	}
}

func curl(addr, method, path string, body io.Reader) (*http.Response, string) {
	req, err := http.NewRequest(method, "http://"+addr+path, body)
	Expect(err).ToNot(HaveOccurred())
	resp, err := http.DefaultClient.Do(req)
	Expect(err).ToNot(HaveOccurred())
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	Expect(err).ToNot(HaveOccurred())
	return resp, string(b)
}

var _ = Describe("server command", func() {
	Describe("validate", func() {
		It("requires --queue unless --allow-create-default", func() {
			c := &serverCommand{}
			Expect(c.validate()).To(MatchError(ContainSubstring("--queue is required")))
		})

		It("requires both TLS files together", func() {
			c := &serverCommand{allowCreateDefault: true, tlsCert: "/tmp/cert"}
			Expect(c.validate()).To(MatchError(ContainSubstring("--tls-cert and --tls-key")))
		})

		It("rejects --tls-client-ca without --tls-cert", func() {
			c := &serverCommand{allowCreateDefault: true, tlsClientCA: "/tmp/ca"}
			Expect(c.validate()).To(MatchError(ContainSubstring("--tls-client-ca requires --tls-cert")))
		})

		It("accepts a valid combination", func() {
			c := &serverCommand{allowCreateDefault: true}
			Expect(c.validate()).To(Succeed())
		})
	})

	Describe("end-to-end Integration", func() {
		It("serves the meta, queues, schedules, and tasks endpoints", func() {
			withJetStream(func(nc *nats.Conn) {
				cmd := &serverCommand{
					allowCreateDefault: true,
					readTimeout:        5 * time.Second,
					writeTimeout:       5 * time.Second,
					maxBody:            64 * 1024,
				}

				addr, stop := runServer(nc, cmd)
				defer stop()

				By("serving /healthz")
				resp, body := curl(addr, http.MethodGet, "/healthz", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(body).To(ContainSubstring(`"status":"ok"`))

				By("serving /v1/openapi.json")
				resp, body = curl(addr, http.MethodGet, "/v1/openapi.json", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(body).To(ContainSubstring(`"openapi"`))

				By("returning info")
				resp, body = curl(addr, http.MethodGet, "/v1/info", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(body).To(ContainSubstring(`"version"`))
				Expect(body).To(ContainSubstring(`"auth":"none"`))

				By("creating a queue")
				resp, _ = curl(addr, http.MethodPost, "/v1/queues", strings.NewReader(`{"name":"Q1"}`))
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))

				By("listing queues including the new one")
				resp, body = curl(addr, http.MethodGet, "/v1/queues", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(body).To(ContainSubstring(`"Q1"`))

				By("fetching detail for the new queue")
				resp, body = curl(addr, http.MethodGet, "/v1/queues/Q1", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(body).To(ContainSubstring(`"Q1"`))

				By("creating a scheduled task")
				body = `{"name":"sched1","schedule":"@every 1m","queue":"Q1","task_type":"email:new","payload":{"to":"a"}}`
				resp, raw := curl(addr, http.MethodPost, "/v1/schedules", strings.NewReader(body))
				Expect(resp.StatusCode).To(Equal(http.StatusCreated), raw)

				By("listing schedules")
				resp, body = curl(addr, http.MethodGet, "/v1/schedules", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(body).To(ContainSubstring(`"sched1"`))

				By("deleting the schedule")
				resp, _ = curl(addr, http.MethodDelete, "/v1/schedules/sched1", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

				By("creating a task")
				resp, raw = curl(addr, http.MethodPost, "/v1/tasks", strings.NewReader(`{"type":"email:new","payload":{"to":"b"}}`))
				Expect(resp.StatusCode).To(Equal(http.StatusCreated), raw)

				var created struct {
					ID string `json:"id"`
				}
				Expect(json.Unmarshal([]byte(raw), &created)).To(Succeed())
				Expect(created.ID).ToNot(BeEmpty())

				By("fetching the created task")
				resp, body = curl(addr, http.MethodGet, fmt.Sprintf("/v1/tasks/%s", created.ID), nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(body).To(ContainSubstring(created.ID))

				By("listing tasks")
				resp, body = curl(addr, http.MethodGet, "/v1/tasks?limit=10", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(body).To(ContainSubstring(created.ID))

				By("returning retry policies")
				resp, body = curl(addr, http.MethodGet, "/v1/retry-policies", nil)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(body).To(ContainSubstring(`"policies"`))
			})
		})
	})
})
