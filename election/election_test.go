// Copyright (c) 2021, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package election

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLeader(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Election")
}

var _ = Describe("Leader Election", func() {
	var (
		srv      *server.Server
		nc       *nats.Conn
		js       nats.KeyValueManager
		kv       nats.KeyValue
		err      error
		debugger func(f string, a ...any)
	)

	BeforeEach(func() {
		skipValidate = false
		skipSplay = false
		srv, nc = startJSServer(GinkgoT())
		js, err = nc.JetStream()
		Expect(err).ToNot(HaveOccurred())

		kv, err = js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket: "LEADER_ELECTION",
			TTL:    750 * time.Millisecond,
		})
		Expect(err).ToNot(HaveOccurred())
		debugger = func(f string, a ...any) {
			fmt.Fprintf(GinkgoWriter, fmt.Sprintf("%s\n", f), a...)
		}
	})

	AfterEach(func() {
		nc.Close()
		srv.Shutdown()
		srv.WaitForShutdown()
		if srv.StoreDir() != "" {
			os.RemoveAll(srv.StoreDir())
		}
	})

	Describe("Election", func() {
		It("Should validate the TTL", func() {
			kv, err := js.CreateKeyValue(&nats.KeyValueConfig{
				Bucket: "LE",
				TTL:    24 * time.Hour,
			})
			Expect(err).ToNot(HaveOccurred())

			_, err = NewElection("test", "test.key", kv)
			Expect(err).To(MatchError("bucket TTL should be less than or equal to 1 hour"))
		})

		It("Should correctly manage leadership", func() {
			var (
				wins      = 0
				lost      = 0
				active    = make(map[string]struct{})
				maxActive = 0
				other     = 0
				wg        = &sync.WaitGroup{}
				mu        = sync.Mutex{}
			)

			skipValidate = true

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			worker := func(wg *sync.WaitGroup, i int, key string) {
				defer wg.Done()

				name := fmt.Sprintf("member %d", i)

				winCb := func() {
					mu.Lock()
					wins++
					active[name] = struct{}{}
					act := len(active)
					if act > maxActive {
						maxActive = act
					}
					mu.Unlock()

					debugger("%d became leader with %d active leaders", i, act)
				}

				lostCb := func() {
					mu.Lock()
					lost++
					delete(active, name)
					mu.Unlock()
					debugger("%d lost leadership", i)
				}

				elect, err := NewElection(name, key, kv,
					OnWon(winCb),
					OnLost(lostCb),
					WithDebug(debugger))
				Expect(err).ToNot(HaveOccurred())

				err = elect.Start(ctx)
				Expect(err).ToNot(HaveOccurred())
			}

			for i := 0; i < 10; i++ {
				wg.Add(1)
				go worker(wg, i, "election")
			}

			// make sure another election is not interrupted
			otherWorker := func(wg *sync.WaitGroup, i int) {
				defer wg.Done()

				elect, err := NewElection(fmt.Sprintf("other %d", i), "other", kv,
					OnWon(func() {
						mu.Lock()
						debugger("other gained leader")
						other++
						mu.Unlock()
					}),
					OnLost(func() {
						defer GinkgoRecover()

						// lostCb fires during shutdown for whichever instance was leader
						if ctx.Err() != nil {
							return
						}
						Fail("Other election was lost")
					}))
				Expect(err).ToNot(HaveOccurred())

				err = elect.Start(ctx)
				Expect(err).ToNot(HaveOccurred())
			}
			wg.Add(2)
			go otherWorker(wg, 1)
			go otherWorker(wg, 2)

			// test failure scenarios, either the key gets deleted to allow a Create() to succeed
			// or it gets corrupted by putting a item in the key that would therefore change the sequence
			// causing next campaign by the leader to fail. The leader will stand-down, all campaigns will
			// fail until the corruption is removed by the MaxAge limit
			kills := 0
			for {
				if ctxSleep(ctx, 100*time.Millisecond) != nil {
					break
				}

				mu.Lock()
				act := len(active)
				mu.Unlock()

				// only corrupt when there is a leader
				if act == 0 {
					continue
				}

				kills++
				if kills%3 == 0 {
					debugger("deleting key")
					Expect(kv.Delete("election")).ToNot(HaveOccurred())
				} else {
					debugger("corrupting key")
					_, err := kv.Put("election", nil)
					Expect(err).ToNot(HaveOccurred())
				}
			}

			wg.Wait()

			mu.Lock()
			defer mu.Unlock()

			// check we had enough keys and wins etc to have tested all scenarios
			if kills < 4 {
				Fail(fmt.Sprintf("had %d kills", kills))
			}
			if wins < 4 {
				Fail(fmt.Sprintf("won only %d elections for %d kills", wins, kills))
			}
			if lost < 4 {
				Fail(fmt.Sprintf("lost only %d elections", lost))
			}
			if maxActive > 1 {
				Fail(fmt.Sprintf("Had %d leaders", maxActive))
			}
		})

		It("Should unblock a running Start when Stop is called", func() {
			skipValidate = true
			skipSplay = true

			elect, err := NewElection("n1", "stop.key", kv, WithDebug(debugger))
			Expect(err).ToNot(HaveOccurred())

			done := make(chan error, 1)
			go func() {
				done <- elect.Start(context.Background())
			}()

			time.Sleep(500 * time.Millisecond)
			elect.Stop()

			Eventually(done, 5*time.Second).Should(Receive(BeNil()))
		})

		It("Should report state transitions via State and IsLeader", func() {
			skipValidate = true
			skipSplay = true

			elect, err := NewElection("n1", "state.key", kv, WithDebug(debugger))
			Expect(err).ToNot(HaveOccurred())

			// pre-Start
			Expect(elect.IsLeader()).To(BeFalse())
			Expect(elect.State()).To(Equal(UnknownState))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() { elect.Start(ctx) }()

			// with no competition IsLeader must become true after the first successful maintain tick
			Eventually(elect.IsLeader, 3*time.Second, 50*time.Millisecond).Should(BeTrue())
			Expect(elect.State()).To(Equal(LeaderState))
		})

		It("Should fire OnLost during shutdown when leader", func() {
			skipValidate = true
			skipSplay = true

			var wins, losses int32
			wonCh := make(chan struct{}, 1)

			elect, err := NewElection("n1", "shutdown.key", kv,
				OnWon(func() {
					atomic.AddInt32(&wins, 1)
					select {
					case wonCh <- struct{}{}:
					default:
					}
				}),
				OnLost(func() {
					atomic.AddInt32(&losses, 1)
				}),
				WithDebug(debugger))
			Expect(err).ToNot(HaveOccurred())

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				defer close(done)
				elect.Start(ctx)
			}()

			Eventually(wonCh, 3*time.Second).Should(Receive())
			cancel()
			Eventually(done, 3*time.Second).Should(BeClosed())

			Expect(atomic.LoadInt32(&wins)).To(Equal(int32(1)))
			Expect(atomic.LoadInt32(&losses)).To(Equal(int32(1)))
		})

		It("Should not fire OnLost when leadership is lost before announcement", func() {
			skipValidate = true
			skipSplay = true

			var (
				wins         int32
				losses       int32
				leaderSeen   int32
				sabotageOnce sync.Once
				sabotageDone = make(chan struct{})
			)

			elect, err := NewElection("n1", "preannounce.key", kv,
				OnWon(func() { atomic.AddInt32(&wins, 1) }),
				OnLost(func() { atomic.AddInt32(&losses, 1) }),
				OnCampaign(func(state State) {
					// The first time a tick runs in Leader state we are
					// between Create-success and the first maintain. Delete
					// the key so the imminent Update fails — this must NOT
					// fire OnLost because OnWon was never announced.
					if state == LeaderState && atomic.CompareAndSwapInt32(&leaderSeen, 0, 1) {
						sabotageOnce.Do(func() {
							Expect(kv.Delete("preannounce.key")).ToNot(HaveOccurred())
							close(sabotageDone)
						})
					}
				}),
				WithDebug(debugger))
			Expect(err).ToNot(HaveOccurred())

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			go func() { elect.Start(ctx) }()

			Eventually(sabotageDone, 3*time.Second).Should(BeClosed())
			// give the leader one more tick (cInterval ≈ 562ms at 750ms TTL) to attempt and fail the maintain
			time.Sleep(1500 * time.Millisecond)

			Expect(atomic.LoadInt32(&losses)).To(Equal(int32(0)), "OnLost must not fire before OnWon was announced")
		})
	})
})

func startJSServer(t GinkgoTInterface) (*server.Server, *nats.Conn) {
	t.Helper()

	d, err := os.MkdirTemp("", "jstest")
	if err != nil {
		t.Fatalf("temp dir could not be made: %s", err)
	}

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

	nc, err := nats.Connect(s.ClientURL(), nats.UseOldRequestStyle())
	if err != nil {
		t.Fatalf("client start failed: %s", err)
	}

	return s, nc
}
