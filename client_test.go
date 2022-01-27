// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAsyncJobs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AsyncJobs")
}

func ExampleNewTask_basic() {
	email := map[string]interface{}{
		"to":      "user@example.net",
		"subject": "This is a test",
		"body":    "Sample Email",
	}

	task, err := NewTask("email:send", email)
	if err != nil {
		panic(fmt.Sprintf("Could not create task: %v", err))
	}

	fmt.Printf("Task ID: %s\n", task.ID)
}

func withJetStream(cb func(nc *nats.Conn, mgr *jsm.Manager)) {
	d, err := ioutil.TempDir("", "jstest")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(d)

	opts := &server.Options{
		JetStream: true,
		StoreDir:  d,
		Port:      -1,
		Host:      "localhost",
		LogFile:   "/dev/stdout",
		Trace:     true,
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

	mgr, err := jsm.New(nc, jsm.WithTimeout(time.Second))
	Expect(err).ToNot(HaveOccurred())

	cb(nc, mgr)
}

var _ = Describe("Client", func() {
	It("Should function", func() {
		withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
			client, err := NewClient(NatsConn(nc))
			Expect(err).ToNot(HaveOccurred())

			testCount := 100
			firstID := ""

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			for i := 0; i < testCount; i++ {
				task, err := NewTask("test", map[string]string{"hello": "world"})
				Expect(err).ToNot(HaveOccurred())

				if firstID == "" {
					firstID = task.ID
				}

				err = client.EnqueueTask(ctx, "DEFAULT", task)
				Expect(err).ToNot(HaveOccurred())
			}

			log.Printf("Starting processing")

			handled := int32(0)

			router := NewTaskRouter()
			router.HandleFunc("test", func(ctx context.Context, t *Task) ([]byte, error) {
				if t.Tries > 1 {
					log.Printf("Try %d for task %s", t.Tries, t.ID)
				}

				done := atomic.AddInt32(&handled, 1)
				if done == int32(testCount) {
					log.Printf("Processed all messages")
					time.AfterFunc(50*time.Millisecond, func() {
						cancel()
					})
				} else if done > 0 && done%100 == 0 {
					return nil, fmt.Errorf("simulated error on %d", done)
				}

				return []byte("done"), nil
			})

			err = client.Run(ctx, router)
			if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
				Fail(fmt.Sprintf("client run failed: %s", err))
			}

			task, err := client.LoadTaskByID(firstID)
			if err != nil {
				Fail(fmt.Sprintf("load failed: %v", err))
			}
			if task.State == TaskStateCompleted {
				tj, _ := json.MarshalIndent(task, "", "  ")
				fmt.Printf("Task: %s\n", string(tj))
				fmt.Printf("%s task %s @ %s result: %q", task.State, task.ID, task.Result.CompletedAt, task.Result.Payload)
			} else {
				fmt.Printf("error task: %#v", task)
			}
		})
	})
})
