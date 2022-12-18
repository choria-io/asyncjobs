// Copyright (c) 2021, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package election

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
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

var skipValidate bool

func NewElection(name string, key string, bucket nats.KeyValue, opts ...Option) (Election, error) {
	e := &election{
		state:   UnknownState,
		lastSeq: math.MaxUint64,
		opts: &options{
			name:   name,
			key:    key,
			bucket: bucket,
		},
	}

	status, err := bucket.Status()
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

func (e *election) campaignForLeadership() error {
	campaignsCounter.WithLabelValues(e.opts.key, e.opts.name, stateNames[CandidateState]).Inc()

	seq, err := e.opts.bucket.Create(e.opts.key, []byte(e.opts.name))
	if err != nil {
		e.tries++
		return nil
	}

	e.lastSeq = seq
	e.state = LeaderState
	e.tries = 0
	e.notifyNext = true // sets state that would notify about win on next campaign
	leaderGauge.WithLabelValues(e.opts.key, e.opts.name).Set(1)

	return nil
}

func (e *election) maintainLeadership() error {
	campaignsCounter.WithLabelValues(e.opts.key, e.opts.name, stateNames[LeaderState]).Inc()

	seq, err := e.opts.bucket.Update(e.opts.key, []byte(e.opts.name), e.lastSeq)
	if err != nil {
		e.debugf("key update failed, moving to candidate state: %v", err)
		e.state = CandidateState
		e.lastSeq = math.MaxUint64

		leaderGauge.WithLabelValues(e.opts.key, e.opts.name).Set(0)

		if e.opts.lostCb != nil {
			e.opts.lostCb()
		}

		return err
	}
	e.lastSeq = seq

	// we wait till the next campaign to notify that we are leader to give others a chance to stand down
	if e.notifyNext {
		e.notifyNext = false
		if e.opts.wonCb != nil {
			ctxSleep(e.ctx, 200*time.Millisecond)
			e.opts.wonCb()
		}
	}

	return nil
}

func (e *election) try() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.opts.campaignCb != nil {
		e.opts.campaignCb(e.state)
	}

	switch e.state {
	case LeaderState:
		return e.maintainLeadership()

	case CandidateState:
		return e.campaignForLeadership()

	default:
		return fmt.Errorf("campaigned while in unknown state")
	}
}

func (e *election) campaign(wg *sync.WaitGroup) error {
	defer wg.Done()

	e.mu.Lock()
	e.state = CandidateState
	e.mu.Unlock()

	// spread out startups a bit
	splay := time.Duration(rand.Intn(5000)) * time.Millisecond
	ctxSleep(e.ctx, splay)

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
			e.stop()

			if e.opts.lostCb != nil && e.IsLeader() {
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
	defer e.mu.Unlock()

	if !e.started {
		return
	}

	if e.cancel != nil {
		e.cancel()
	}

	e.stop()
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
