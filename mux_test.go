// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Router", func() {
	Describe("Handler", func() {
		It("Should support default handler", func() {
			router := NewTaskRouter()
			router.HandleFunc("x", func(_ context.Context, _ *Task) (interface{}, error) {
				return "x", nil
			})

			task, err := NewTask("y", nil)
			Expect(err).ToNot(HaveOccurred())

			handler := router.Handler(task)
			_, err = handler(nil, task)
			Expect(err).To(MatchError("no handler for task type y"))
		})

		It("Should find the correct handler", func() {
			router := NewTaskRouter()
			router.HandleFunc("", func(_ context.Context, _ *Task) (interface{}, error) {
				return "custom default", nil
			})
			router.HandleFunc("things:", func(_ context.Context, _ *Task) (interface{}, error) {
				return "things:", nil
			})
			router.HandleFunc("things:very:specific", func(_ context.Context, _ *Task) (interface{}, error) {
				return "things:very:specific", nil
			})
			router.HandleFunc("things:specific", func(_ context.Context, _ *Task) (interface{}, error) {
				return "things:specific", nil
			})

			check := func(ttype string, expected string) {
				task := &Task{Type: ttype}
				res, err := router.Handler(task)(context.Background(), task)
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
})
