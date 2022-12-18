// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RequestReplyHandler", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		log.SetOutput(GinkgoWriter)
	})

	AfterEach(func() { cancel() })

	It("Should handle support catchall", func() {
		withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
			task, err := NewTask("email:new", "testing")

			go func() {
				nc.Subscribe(fmt.Sprintf(RequestReplyTaskHandlerPattern, "catchall"), func(msg *nats.Msg) {
					resp := nats.NewMsg(msg.Subject)
					resp.Header.Add(RequestReplyError, "test error")
					resp.Data = []byte("error")
					msg.RespondMsg(resp)
				})

				<-ctx.Done()
			}()

			time.Sleep(20 * time.Millisecond)

			rrh := newRequestReplyHandleFunc(nc, "")
			Expect(err).ToNot(HaveOccurred())
			payload, err := rrh(ctx, &defaultLogger{}, task)
			Expect(err).To(MatchError(fmt.Errorf("test error")))
			Expect(payload).To(Equal([]byte("error")))
		})
	})

	It("Should handle normal headers", func() {
		withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
			task, err := NewTask("email:new", "testing")

			go func() {
				nc.Subscribe(fmt.Sprintf(RequestReplyTaskHandlerPattern, "email:new"), func(msg *nats.Msg) {
					resp := nats.NewMsg(msg.Subject)
					resp.Header.Add(RequestReplyError, "test error")
					resp.Data = []byte("error")
					msg.RespondMsg(resp)
				})

				<-ctx.Done()
			}()

			time.Sleep(20 * time.Millisecond)

			rrh := newRequestReplyHandleFunc(nc, "email:new")
			Expect(err).ToNot(HaveOccurred())
			payload, err := rrh(ctx, &defaultLogger{}, task)
			Expect(err).To(MatchError(fmt.Errorf("test error")))
			Expect(payload).To(Equal([]byte("error")))
		})
	})

	It("Should handle termination headers", func() {
		withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
			task, err := NewTask("email:new", "testing")

			go func() {
				nc.Subscribe(fmt.Sprintf(RequestReplyTaskHandlerPattern, "email:new"), func(msg *nats.Msg) {
					resp := nats.NewMsg(msg.Subject)
					resp.Header.Add(RequestReplyTerminateError, "test termination")
					resp.Data = []byte("error")
					msg.RespondMsg(resp)
				})

				<-ctx.Done()
			}()

			time.Sleep(20 * time.Millisecond)

			rrh := newRequestReplyHandleFunc(nc, "email:new")
			Expect(err).ToNot(HaveOccurred())
			payload, err := rrh(ctx, &defaultLogger{}, task)
			Expect(err).To(MatchError(ErrTerminateTask))
			Expect(payload).To(Equal([]byte("error")))
		})
	})

	It("Should call correctly with the right headers", func() {
		withJetStream(func(nc *nats.Conn, _ *jsm.Manager) {
			task, err := NewTask("email:new", "testing")

			go func() {
				nc.Subscribe(fmt.Sprintf(RequestReplyTaskHandlerPattern, "email:new"), func(msg *nats.Msg) {
					fail := func(format string, a ...any) {
						resp := nats.NewMsg(msg.Subject)
						resp.Header.Add(RequestReplyError, fmt.Sprintf(format, a...))
						msg.RespondMsg(resp)
					}

					ct := msg.Header.Get(RequestReplyContentTypeHeader)
					deadline, err := time.Parse(time.RFC3339, msg.Header.Get(RequestReplyDeadlineHeader))
					if err != nil {
						fail("invalid deadline: %v", deadline)
						return
					}
					if ct != RequestReplyTaskType {
						fail("invalid content type %v", ct)
						return
					}
					if time.Until(deadline) < 1500*time.Millisecond {
						fail("invalid deadline %v (%v)", deadline, time.Until(deadline))
						return
					}

					t := &Task{}
					err = json.Unmarshal(msg.Data, t)
					if err != nil {
						fail("invalid task json: %v", err)
						return
					}
					if t.ID != task.ID {
						fail("invalid task content: %#v", t)
						return
					}

					msg.Respond([]byte("ok"))
				})

				<-ctx.Done()
			}()

			time.Sleep(20 * time.Millisecond)
			rrh := newRequestReplyHandleFunc(nc, "email:new")
			Expect(err).ToNot(HaveOccurred())

			_, err = rrh(context.Background(), &defaultLogger{}, task)
			Expect(err).To(MatchError(ErrRequestReplyNoDeadline))

			short, cancel := context.WithTimeout(ctx, time.Second)
			_, err = rrh(short, &defaultLogger{}, task)
			cancel()
			Expect(err).To(MatchError(ErrRequestReplyShortDeadline))

			payload, err := rrh(ctx, &defaultLogger{}, task)
			Expect(err).ToNot(HaveOccurred())
			Expect(payload).To(Equal([]byte("ok")))
		})
	})
})
