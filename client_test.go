package jsaj

import (
	"context"
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
)

func withJetStream(t *testing.T, cb func(t *testing.T, nc *nats.Conn, mgr *jsm.Manager)) {
	t.Helper()

	d, err := ioutil.TempDir("", "jstest")
	if err != nil {
		t.Fatalf("temp dir could not be made: %s", err)
	}
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
	if err != nil {
		t.Fatal("server start failed: ", err)
	}

	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		t.Error("nats server did not start")
	}
	defer func() {
		s.Shutdown()
		s.WaitForShutdown()
	}()

	nc, err := nats.Connect(s.ClientURL(), nats.UseOldRequestStyle())
	if err != nil {
		t.Fatalf("client start failed: %s", err)
	}
	defer nc.Close()

	mgr, err := jsm.New(nc, jsm.WithTimeout(time.Second))
	if err != nil {
		t.Fatalf("manager creation failed: %s", err)
	}

	cb(t, nc, mgr)
}

func TestClient(t *testing.T) {
	withJetStream(t, func(t *testing.T, nc *nats.Conn, mgr *jsm.Manager) {
		client, err := NewClient(nc)
		if err != nil {
			t.Fatalf("client failed: %v", err)
		}

		testCount := 1000
		firstID := ""

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		for i := 0; i < testCount; i++ {
			task, err := NewTask("test", map[string]string{"hello": "world"})
			if err != nil {
				t.Fatalf("task failed: %v", err)
			}

			if firstID == "" {
				firstID = task.ID
			}

			err = client.EnqueueTask(ctx, "DEFAULT", task)
			if err != nil {
				t.Fatalf("enqueue failed: %v", err)
			}
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
			t.Fatalf("client run failed: %s", err)
		}

		task, err := client.LoadTaskByID(firstID)
		if err != nil {
			t.Fatalf("load failed: %v", err)
		}
		if task.State == TaskStateCompleted {
			fmt.Printf("%s task %s @ %s result: %q", task.State, task.ID, task.Result.CompletedAt, task.Result.Payload)
		} else {
			fmt.Printf("error task: %#v", task)
		}
	})
}
