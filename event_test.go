package asyncjobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	timeout = 1 // sec
	topic   = "test"
)

var _ = Describe("Events", func() {
	BeforeEach(func() {
		log.SetOutput(GinkgoWriter)
	})

	It("Should occur for a compelted task", func() {
		withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
			client, err := NewClient(NatsConn(nc))
			Expect(err).ToNot(HaveOccurred())

			testCount := 1
			firstID := ""

			ctx, cancel := context.WithTimeout(context.Background(), timeout*time.Second)
			defer cancel()

			for range testCount {
				task, err := NewTask(topic, map[string]string{"hello": "world"})
				Expect(err).ToNot(HaveOccurred())

				if firstID == "" {
					firstID = task.ID
				}

				err = client.EnqueueTask(ctx, task)
				Expect(err).ToNot(HaveOccurred())
			}

			router := NewTaskRouter()
			router.HandleFunc(topic, func(ctx context.Context, _ Logger, _ *Task) (any, error) {
				return "success", nil
			})

			subscription, err := nc.SubscribeSync(EventsSubjectWildcard)
			Expect(err).ToNot(HaveOccurred())

			// run client
			go func() {
				err = client.Run(ctx, router)
				Expect(err).ToNot(HaveOccurred())
			}()

			// run event listener
			for {
				msg, err := subscription.NextMsg(timeout * time.Second)
				if err != nil {
					if errors.Is(err, nats.ErrTimeout) {
						// it's okay
						break
					} else {
						panic(err)
					}
				}

				event, kind, err := ParseEventJSON(msg.Data)
				Expect(err).ToNot(HaveOccurred())

				switch e := event.(type) {
				case TaskStateChangeEvent:
					if e.LastErr == "" {
						fmt.Printf("[%s] %s: queue: %s type: %s tries: %d state: %s\n", e.TimeStamp.Format("15:04:05"), e.TaskID, e.Queue, e.TaskType, e.Tries, e.State)
					} else {
						fmt.Printf("[%s] %s: queue: %s type: %s tries: %d state: %s error: %s\n", e.TimeStamp.Format("15:04:05"), e.TaskID, e.Queue, e.TaskType, e.Tries, e.State, e.LastErr)
					}

				case LeaderElectedEvent:
					fmt.Printf("[%s] %s: new %s leader\n", e.TimeStamp.Format("15:04:05"), e.Name, e.Component)

				default:
					fmt.Printf("[%s] Unknown event type %s\n", time.Now().UTC().Format("15:04:05"), kind)
				}
			}
		})
	})

	It("Should occur for a terminated task", func() {
		withJetStream(func(nc *nats.Conn, mgr *jsm.Manager) {
			client, err := NewClient(NatsConn(nc))
			Expect(err).ToNot(HaveOccurred())

			testCount := 1
			firstID := ""

			ctx, cancel := context.WithTimeout(context.Background(), timeout*time.Second)
			defer cancel()

			for range testCount {
				task, err := NewTask(topic, map[string]string{"hello": "world"})
				Expect(err).ToNot(HaveOccurred())

				if firstID == "" {
					firstID = task.ID
				}

				err = client.EnqueueTask(ctx, task)
				Expect(err).ToNot(HaveOccurred())
			}

			router := NewTaskRouter()
			router.HandleFunc(topic, func(ctx context.Context, _ Logger, _ *Task) (any, error) {
				return nil, ErrTerminateTask
			})

			subscription, err := nc.SubscribeSync(EventsSubjectWildcard)
			Expect(err).ToNot(HaveOccurred())

			// run client
			go func() {
				err = client.Run(ctx, router)
				Expect(err).ToNot(HaveOccurred())
			}()

			// run event listener
			for {
				msg, err := subscription.NextMsg(timeout * time.Second)
				if err != nil {
					if errors.Is(err, nats.ErrTimeout) {
						// it's okay
						break
					} else {
						panic(err)
					}
				}

				event, kind, err := ParseEventJSON(msg.Data)
				Expect(err).ToNot(HaveOccurred())

				switch e := event.(type) {
				case TaskStateChangeEvent:
					if e.LastErr == "" {
						fmt.Printf("[%s] %s: queue: %s type: %s tries: %d state: %s\n", e.TimeStamp.Format("15:04:05"), e.TaskID, e.Queue, e.TaskType, e.Tries, e.State)
					} else {
						fmt.Printf("[%s] %s: queue: %s type: %s tries: %d state: %s error: %s\n", e.TimeStamp.Format("15:04:05"), e.TaskID, e.Queue, e.TaskType, e.Tries, e.State, e.LastErr)
					}

				case LeaderElectedEvent:
					fmt.Printf("[%s] %s: new %s leader\n", e.TimeStamp.Format("15:04:05"), e.Name, e.Component)

				default:
					fmt.Printf("[%s] Unknown event type %s\n", time.Now().UTC().Format("15:04:05"), kind)
				}
			}
		})
	})
})
