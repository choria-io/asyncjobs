// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TaskScheduler", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		wg     sync.WaitGroup
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	})

	AfterEach(func() { cancel() })

	Describe("Run", func() {
		It("Should function", func() {
			withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
				client, err := NewClient(NatsConn(nc))
				Expect(err).ToNot(HaveOccurred())

				task, _ := NewTask("ginko:test", nil)
				Expect(client.NewScheduledTask("ginkgo", "@every 1s", "DEFAULT", task)).ToNot(HaveOccurred())

				scheduler, err := NewTaskScheduler("ginkgo", client)
				Expect(err).ToNot(HaveOccurred())
				scheduler.skipLeaderElection = true
				Expect(scheduler.Run(ctx, &wg)).ToNot(HaveOccurred())
				scheduler.Stop()

				tasks, err := client.StorageAdmin().TasksInfo()
				Expect(err).ToNot(HaveOccurred())
				log.Printf("msgs: %d", tasks.Stream.State.Msgs)
				Expect(tasks.Stream.State.Msgs).To( // depends when the test is started exactly
					And(
						BeNumerically(">=", uint64(4)),
						BeNumerically("<=", uint64(6))))
			})
		})
	})
})
