// Copyright (c) 2021, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package election implements a simple leader election on top of a
// NATS JetStream Key-Value bucket.
//
// Each candidate runs a campaign loop against a single key in a KV bucket
// whose bucket TTL determines how long a stale leader record survives.
// Elections progress in two roles:
//
//   - Candidate: attempts to Create the key. Create is atomic in JetStream
//     and will succeed for exactly one candidate when the key is absent
//     (either never written, or expired via the bucket TTL). On success the
//     candidate becomes leader; on failure it waits for the next tick.
//
//   - Leader: periodically Updates the key using compare-and-swap against
//     the last observed revision. Refreshing the key keeps it from
//     expiring. If the CAS fails (key deleted, overwritten, or another
//     writer raced in) the leader immediately steps down to candidate and
//     lets the TTL adjudicate the next round.
//
// The campaign interval defaults to 75% of the bucket TTL so a leader gets
// multiple refresh attempts before its record can expire. Candidates sleep
// a small random splay on startup to reduce thundering-herd Create
// contention, and an optional Backoff can lengthen the interval while a
// candidate is losing campaigns.
//
// After winning, leadership is not announced until the next successful
// maintain tick — this gives a prior leader time to observe its own CAS
// failure and run OnLost before OnWon fires elsewhere, keeping observers
// consistent with the single-writer invariant the KV enforces.
//
// Safety comes from JetStream: Create and revisioned Update are atomic, so
// at most one candidate holds the key at any instant. Liveness comes from
// the TTL: if a leader dies without stepping down, the key expires and a
// new election proceeds. There is a brief window (up to one TTL) during
// which no leader exists; callers must tolerate this.
package election

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Backoff controls the interval of campaigns
type Backoff interface {
	// Duration returns the time to sleep for the nth invocation
	Duration(n int) time.Duration
}

// State indicates the current state of the election
type State uint

// Election manages leader elections
type Election interface {
	Start(ctx context.Context) error
	Stop()
	IsLeader() bool
	State() State
}

const (
	// UnknownState indicates the state is unknown, like when the election is not started
	UnknownState State = 0
	// CandidateState is a campaigner that is not the leader
	CandidateState State = 1
	// LeaderState is the leader
	LeaderState State = 2
)

var (
	stateNames = map[State]string{
		UnknownState:   "unknown",
		CandidateState: "candidate",
		LeaderState:    "leader",
	}
)

func (s State) String() string {
	return stateNames[s]
}

// implements Election
type election struct {
	opts  *options
	state State

	ctx        context.Context
	cancel     context.CancelFunc
	started    bool
	lastSeq    uint64
	tries      int
	notifyNext bool

	mu sync.Mutex
}

var (
	skipValidate bool
	skipSplay    bool
)

func NewElection(name string, key string, bucket jetstream.KeyValue, opts ...Option) (Election, error) {
	e := &election{
		state:   UnknownState,
		lastSeq: math.MaxUint64,
		opts: &options{
			name:   name,
			key:    key,
			bucket: bucket,
		},
	}

	sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer scancel()

	status, err := bucket.Status(sctx)
	if err != nil {
		return nil, err
	}

	e.opts.ttl = status.TTL()
	if !skipValidate {
		if e.opts.ttl < 10*time.Second {
			return nil, fmt.Errorf("bucket TTL should be 10 seconds or more")
		}
		if e.opts.ttl > time.Hour {
			return nil, fmt.Errorf("bucket TTL should be less than or equal to 1 hour")
		}
	}

	e.opts.cInterval = time.Duration(float64(e.opts.ttl) * 0.75)

	for _, opt := range opts {
		opt(e.opts)
	}

	if !skipValidate {
		if e.opts.cInterval.Seconds() < 5 {
			return nil, fmt.Errorf("campaign interval %v too small", e.opts.cInterval)
		}
	}

	e.debugf("Campaign interval: %v", e.opts.cInterval)

	return e, nil
}

func (e *election) debugf(format string, a ...any) {
	if e.opts.debug == nil {
		return
	}
	e.opts.debug(format, a...)
}

// caller must hold e.mu
func (e *election) campaignForLeadershipLocked() error {
	campaignsCounter.WithLabelValues(e.opts.key, e.opts.name, stateNames[CandidateState]).Inc()

	seq, err := e.opts.bucket.Create(e.ctx, e.opts.key, []byte(e.opts.name))
	if err != nil {
		e.tries++
		e.debugf("campaign create failed: %v", err)
		return nil
	}

	e.lastSeq = seq
	e.state = LeaderState
	e.tries = 0
	e.notifyNext = true // sets state that would notify about win on next campaign
	leaderGauge.WithLabelValues(e.opts.key, e.opts.name).Set(1)

	return nil
}

// caller must hold e.mu. Returns callbacks to invoke after the mutex is released.
func (e *election) maintainLeadershipLocked() (wonCb func(), lostCb func(), err error) {
	campaignsCounter.WithLabelValues(e.opts.key, e.opts.name, stateNames[LeaderState]).Inc()

	seq, err := e.opts.bucket.Update(e.ctx, e.opts.key, []byte(e.opts.name), e.lastSeq)
	if err != nil {
		// Context cancellation is not a CAS failure. Shutdown will drive the
		// stand-down via e.ctx.Done() in campaign(); don't pre-empt it here or
		// we risk double-firing callbacks and mis-labeling the cause.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			e.debugf("update canceled: %v", err)
			return nil, nil, err
		}

		e.debugf("key update failed, moving to candidate state: %v", err)
		e.state = CandidateState
		e.lastSeq = math.MaxUint64
		e.tries = 0

		leaderGauge.WithLabelValues(e.opts.key, e.opts.name).Set(0)

		// only notify loss if a win was actually announced; losing between
		// Create success and the first maintain means the user never saw OnWon
		if e.notifyNext {
			e.notifyNext = false
			return nil, nil, err
		}
		return nil, e.opts.lostCb, err
	}
	e.lastSeq = seq

	// we wait till the next campaign to notify that we are leader to give others a chance to stand down
	if e.notifyNext {
		e.notifyNext = false
		return nil, e.opts.wonCb, nil
	}

	return nil, nil, nil
}

func (e *election) try() error {
	e.mu.Lock()
	currentState := e.state
	campaignCb := e.opts.campaignCb
	e.mu.Unlock()

	if campaignCb != nil {
		campaignCb(currentState)
	}

	e.mu.Lock()
	var (
		err    error
		wonCb  func()
		lostCb func()
	)
	switch e.state {
	case LeaderState:
		wonCb, lostCb, err = e.maintainLeadershipLocked()
	case CandidateState:
		err = e.campaignForLeadershipLocked()
	default:
		err = fmt.Errorf("campaigned while in unknown state")
	}
	e.mu.Unlock()

	if lostCb != nil {
		lostCb()
	}
	if wonCb != nil {
		wonCb()
	}

	return err
}

func (e *election) campaign(wg *sync.WaitGroup) error {
	defer wg.Done()

	e.mu.Lock()
	e.state = CandidateState
	e.mu.Unlock()

	// spread out startups a bit
	if !skipSplay {
		splay := time.Duration(rand.IntN(5000)) * time.Millisecond
		ctxSleep(e.ctx, splay)
	}

	var ticker *time.Ticker
	if e.opts.bo != nil {
		d := e.opts.bo.Duration(0)
		campaignIntervalGauge.WithLabelValues(e.opts.key, e.opts.name).Set(d.Seconds())
		ticker = time.NewTicker(d)
	} else {
		campaignIntervalGauge.WithLabelValues(e.opts.key, e.opts.name).Set(e.opts.cInterval.Seconds())
		ticker = time.NewTicker(e.opts.cInterval)
	}

	tick := func() {
		err := e.try()
		if err != nil {
			e.debugf("election attempt failed: %v", err)
		}

		if e.opts.bo != nil {
			d := e.opts.bo.Duration(e.tries)
			campaignIntervalGauge.WithLabelValues(e.opts.key, e.opts.name).Set(d.Seconds())
			ticker.Reset(d)
		}
	}

	// initial campaign
	tick()

	for {
		select {
		case <-ticker.C:
			tick()

		case <-e.ctx.Done():
			ticker.Stop()

			wasLeader := e.IsLeader()
			e.stop()

			if wasLeader && e.opts.lostCb != nil {
				e.debugf("Calling leader lost during shutdown")
				e.opts.lostCb()
			}

			return nil
		}
	}
}

func (e *election) stop() {
	e.mu.Lock()
	e.started = false
	e.cancel()
	e.state = CandidateState
	e.lastSeq = math.MaxUint64
	e.mu.Unlock()
}

func (e *election) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		return fmt.Errorf("already running")
	}

	e.ctx, e.cancel = context.WithCancel(ctx)
	e.started = true
	e.mu.Unlock()

	wg := &sync.WaitGroup{}
	wg.Add(1)

	err := e.campaign(wg)
	if err != nil {
		e.stop()
		return err
	}

	wg.Wait()

	return nil
}

func (e *election) Stop() {
	e.mu.Lock()
	cancel := e.cancel
	e.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (e *election) IsLeader() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	// it's only leader after the next successful campaign
	return e.state == LeaderState && !e.notifyNext
}

func (e *election) State() State {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.state
}

func ctxSleep(ctx context.Context, duration time.Duration) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	sctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	<-sctx.Done()

	return ctx.Err()
}
