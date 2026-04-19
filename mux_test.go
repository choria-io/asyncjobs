// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func tagMiddleware(trace *[]string, tag string) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, log Logger, t *Task) (any, error) {
			*trace = append(*trace, "before:"+tag)
			res, err := next(ctx, log, t)
			*trace = append(*trace, "after:"+tag)
			return res, err
		}
	}
}

var _ = Describe("Router", func() {
	Describe("ExternalProcess", func() {
		var (
			task   *Task
			err    error
			router *Mux
		)
		BeforeEach(func() {
			task, err = NewTask("email:new", nil)
			Expect(err).ToNot(HaveOccurred())
			router = NewTaskRouter()
		})

		It("Should handle missing commands", func() {
			Expect(router.ExternalProcess("email:new", "testdata/missing.sh")).ToNot(HaveOccurred())
			handler := router.Handler(task)
			_, err = handler(context.Background(), &defaultLogger{}, task)
			Expect(err).To(MatchError(ErrExternalCommandNotFound))
		})

		It("Should handle command failures", func() {
			Expect(router.ExternalProcess("email:new", "testdata/failing-handler.sh")).ToNot(HaveOccurred())
			handler := router.Handler(task)
			_, err = handler(context.Background(), &defaultLogger{}, task)
			Expect(err).To(MatchError(ErrExternalCommandFailed))
		})

		It("Should handle success", func() {
			Expect(router.ExternalProcess("email:new", "testdata/passing-handler.sh")).ToNot(HaveOccurred())
			handler := router.Handler(task)
			payload, err := handler(context.Background(), &defaultLogger{}, task)
			Expect(err).ToNot(HaveOccurred())
			Expect(payload).To(Equal("success\n"))
		})
	})

	Describe("Handler", func() {
		It("Should support default handler", func() {
			router := NewTaskRouter()
			router.HandleFunc("x", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				return "x", nil
			})

			task, err := NewTask("y", nil)
			Expect(err).ToNot(HaveOccurred())

			handler := router.Handler(task)
			_, err = handler(nil, &defaultLogger{}, task)
			Expect(err).To(MatchError(ErrNoHandlerForTaskType))
		})

		It("Should find the correct handler", func() {
			router := NewTaskRouter()
			router.HandleFunc("", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				return "custom default", nil
			})
			router.HandleFunc("things:", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				return "things:", nil
			})
			router.HandleFunc("things:very:specific", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				return "things:very:specific", nil
			})
			router.HandleFunc("things:specific", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				return "things:specific", nil
			})

			check := func(ttype string, expected string) {
				task := &Task{Type: ttype}
				res, err := router.Handler(task)(context.Background(), &defaultLogger{}, task)
				Expect(err).ToNot(HaveOccurred())
				Expect(res).To(Equal(expected))
			}

			check("things:other", "things:")
			check("things:very:other", "things:")
			check("things:very:specific", "things:very:specific")
			check("things:specific", "things:specific")
			check("things:specific:other", "things:specific")
			check("x", "custom default")
		})
	})

	Describe("Middleware", func() {
		var (
			router *Mux
			task   *Task
			err    error
		)

		BeforeEach(func() {
			router = NewTaskRouter()
			task, err = NewTask("email:new", nil)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should apply global middleware to all handlers", func() {
			var trace []string
			Expect(router.Use(tagMiddleware(&trace, "global"))).ToNot(HaveOccurred())
			Expect(router.HandleFunc("email:new", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				trace = append(trace, "handler:a")
				return "a", nil
			})).ToNot(HaveOccurred())
			Expect(router.HandleFunc("email:old", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				trace = append(trace, "handler:b")
				return "b", nil
			})).ToNot(HaveOccurred())

			res, err := router.Handler(task)(context.Background(), &defaultLogger{}, task)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal("a"))
			Expect(trace).To(Equal([]string{"before:global", "handler:a", "after:global"}))

			trace = nil
			otherTask := &Task{Type: "email:old"}
			res, err = router.Handler(otherTask)(context.Background(), &defaultLogger{}, otherTask)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal("b"))
			Expect(trace).To(Equal([]string{"before:global", "handler:b", "after:global"}))
		})

		It("Should apply per-route middleware only to that route", func() {
			var trace []string
			Expect(router.HandleFunc("email:new", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				trace = append(trace, "handler:a")
				return "a", nil
			}, tagMiddleware(&trace, "route"))).ToNot(HaveOccurred())
			Expect(router.HandleFunc("email:old", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				trace = append(trace, "handler:b")
				return "b", nil
			})).ToNot(HaveOccurred())

			_, err := router.Handler(task)(context.Background(), &defaultLogger{}, task)
			Expect(err).ToNot(HaveOccurred())
			Expect(trace).To(Equal([]string{"before:route", "handler:a", "after:route"}))

			trace = nil
			otherTask := &Task{Type: "email:old"}
			_, err = router.Handler(otherTask)(context.Background(), &defaultLogger{}, otherTask)
			Expect(err).ToNot(HaveOccurred())
			Expect(trace).To(Equal([]string{"handler:b"}))
		})

		It("Should let Use take effect when called after HandleFunc", func() {
			var trace []string
			Expect(router.HandleFunc("email:new", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				trace = append(trace, "handler")
				return nil, nil
			})).ToNot(HaveOccurred())
			Expect(router.Use(tagMiddleware(&trace, "late"))).ToNot(HaveOccurred())

			_, err := router.Handler(task)(context.Background(), &defaultLogger{}, task)
			Expect(err).ToNot(HaveOccurred())
			Expect(trace).To(Equal([]string{"before:late", "handler", "after:late"}))
		})

		It("Should run first-registered middleware outermost", func() {
			var trace []string
			Expect(router.Use(
				tagMiddleware(&trace, "outer"),
				tagMiddleware(&trace, "inner"),
			)).ToNot(HaveOccurred())
			Expect(router.HandleFunc("email:new", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				trace = append(trace, "handler")
				return nil, nil
			})).ToNot(HaveOccurred())

			_, err := router.Handler(task)(context.Background(), &defaultLogger{}, task)
			Expect(err).ToNot(HaveOccurred())
			Expect(trace).To(Equal([]string{
				"before:outer", "before:inner", "handler", "after:inner", "after:outer",
			}))
		})

		It("Should run global middleware outside per-route middleware", func() {
			var trace []string
			Expect(router.Use(tagMiddleware(&trace, "global"))).ToNot(HaveOccurred())
			Expect(router.HandleFunc("email:new", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				trace = append(trace, "handler")
				return nil, nil
			}, tagMiddleware(&trace, "route"))).ToNot(HaveOccurred())

			_, err := router.Handler(task)(context.Background(), &defaultLogger{}, task)
			Expect(err).ToNot(HaveOccurred())
			Expect(trace).To(Equal([]string{
				"before:global", "before:route", "handler", "after:route", "after:global",
			}))
		})

		It("Should allow middleware to short-circuit without calling next", func() {
			handlerCalled := false
			deny := errors.New("denied")
			short := func(_ HandlerFunc) HandlerFunc {
				return func(_ context.Context, _ Logger, _ *Task) (any, error) {
					return nil, deny
				}
			}
			Expect(router.HandleFunc("email:new", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				handlerCalled = true
				return "should not run", nil
			}, short)).ToNot(HaveOccurred())

			res, err := router.Handler(task)(context.Background(), &defaultLogger{}, task)
			Expect(err).To(MatchError(deny))
			Expect(res).To(BeNil())
			Expect(handlerCalled).To(BeFalse())
		})

		It("Should reject nil middleware in Use", func() {
			Expect(router.Use(nil)).To(MatchError(ErrInvalidMiddleware))
			ok := tagMiddleware(new([]string), "ok")
			Expect(router.Use(ok, nil)).To(MatchError(ErrInvalidMiddleware))
		})

		It("Should reject nil middleware in HandleFunc", func() {
			err := router.HandleFunc("email:new", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				return nil, nil
			}, nil)
			Expect(err).To(MatchError(ErrInvalidMiddleware))

			_, ok := router.hf["email:new"]
			Expect(ok).To(BeFalse())
		})

		It("Should not wrap the not-found handler", func() {
			var trace []string
			Expect(router.Use(tagMiddleware(&trace, "global"))).ToNot(HaveOccurred())

			unknown := &Task{Type: "no:such:type"}
			_, err := router.Handler(unknown)(context.Background(), &defaultLogger{}, unknown)
			Expect(err).To(MatchError(ErrNoHandlerForTaskType))
			Expect(trace).To(BeEmpty())
		})

		It("Should apply middleware to prefix-matched handlers", func() {
			var trace []string
			Expect(router.Use(tagMiddleware(&trace, "global"))).ToNot(HaveOccurred())
			Expect(router.HandleFunc("things:", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				trace = append(trace, "handler")
				return "things", nil
			}, tagMiddleware(&trace, "route"))).ToNot(HaveOccurred())

			t := &Task{Type: "things:other"}
			res, err := router.Handler(t)(context.Background(), &defaultLogger{}, t)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal("things"))
			Expect(trace).To(Equal([]string{
				"before:global", "before:route", "handler", "after:route", "after:global",
			}))
		})

		It("Should rewrap every existing handler when Use is called late", func() {
			handlers := []string{"a:one", "a:two", "a:three", "a:four"}
			for _, h := range handlers {
				name := h
				Expect(router.HandleFunc(name, func(_ context.Context, _ Logger, _ *Task) (any, error) {
					return name, nil
				})).ToNot(HaveOccurred())
			}

			var trace []string
			Expect(router.Use(tagMiddleware(&trace, "late"))).ToNot(HaveOccurred())

			for _, h := range handlers {
				trace = nil
				t := &Task{Type: h}
				res, err := router.Handler(t)(context.Background(), &defaultLogger{}, t)
				Expect(err).ToNot(HaveOccurred())
				Expect(res).To(Equal(h))
				Expect(trace).To(Equal([]string{"before:late", "after:late"}))
			}
		})

		It("Should be safe under concurrent Use and Handler calls", func() {
			Expect(router.HandleFunc("email:new", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				return "ok", nil
			})).ToNot(HaveOccurred())

			noop := func(next HandlerFunc) HandlerFunc {
				return func(ctx context.Context, log Logger, t *Task) (any, error) {
					return next(ctx, log, t)
				}
			}

			var (
				wg            sync.WaitGroup
				readyOnce     sync.Once
				ready         = make(chan struct{})
				dispatchCount int64
				stop          int32
			)

			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for atomic.LoadInt32(&stop) == 0 {
						res, err := router.Handler(task)(context.Background(), &defaultLogger{}, task)
						Expect(err).ToNot(HaveOccurred())
						Expect(res).To(Equal("ok"))
						atomic.AddInt64(&dispatchCount, 1)
						readyOnce.Do(func() { close(ready) })
					}
				}()
			}

			<-ready
			for i := 0; i < 50; i++ {
				Expect(router.Use(noop)).ToNot(HaveOccurred())
			}
			atomic.StoreInt32(&stop, 1)
			wg.Wait()

			Expect(atomic.LoadInt64(&dispatchCount)).To(BeNumerically(">", 0))
		})

		It("Should compose middleware with Chain in registration order", func() {
			var trace []string
			bundle := Chain(
				tagMiddleware(&trace, "outer"),
				tagMiddleware(&trace, "inner"),
			)
			Expect(router.HandleFunc("email:new", func(_ context.Context, _ Logger, _ *Task) (any, error) {
				trace = append(trace, "handler")
				return nil, nil
			}, bundle)).ToNot(HaveOccurred())

			_, err := router.Handler(task)(context.Background(), &defaultLogger{}, task)
			Expect(err).ToNot(HaveOccurred())
			Expect(trace).To(Equal([]string{
				"before:outer", "before:inner", "handler", "after:inner", "after:outer",
			}))
		})
	})
})
