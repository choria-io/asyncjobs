// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"time"
)

// RetryPolicy defines a period that failed jobs will be retried against
type RetryPolicy struct {
	// Intervals is a range of time periods backoff will be based off
	Intervals []time.Duration
	// Jitter is a factor applied to the specific interval avoid repeating same backoff periods
	Jitter float64
}

// RetryPolicyProvider is the interface that the ReplyPolicy implements,
// use this to implement your own exponential backoff system or similar for
// task retries.
type RetryPolicyProvider interface {
	Duration(n int) time.Duration
}

var (
	// RetryLinearTenMinutes is a 50-step policy between 1 and 10 minutes
	RetryLinearTenMinutes = linearPolicy(50, 0.90, time.Minute, 10*time.Minute)

	// RetryLinearOneHour is a 50-step policy between 10 minutes and 1 hour
	RetryLinearOneHour = linearPolicy(20, 0.90, 10*time.Minute, 60*time.Minute)

	// RetryLinearOneMinute is a 20-step policy between 1 second and 1 minute
	RetryLinearOneMinute = linearPolicy(20, 0.5, time.Second, time.Minute)

	// RetryDefault is the default retry policy
	RetryDefault = RetryLinearTenMinutes

	retryLinearTenSeconds = linearPolicy(20, 0.1, 500*time.Millisecond, 10*time.Second)
	retryForTesting       = linearPolicy(1, 0.1, time.Millisecond, 10*time.Millisecond)

	policies = map[string]RetryPolicyProvider{
		"default": RetryDefault,
		"1m":      RetryLinearOneMinute,
		"10m":     RetryLinearTenMinutes,
		"1h":      RetryLinearOneHour,
	}
)

// RetryPolicyNames returns a list of pre-generated retry policies
func RetryPolicyNames() []string {
	var names []string
	for k := range policies {
		names = append(names, k)
	}

	sort.Strings(names)

	return names
}

// RetryPolicyLookup loads a policy by name
func RetryPolicyLookup(name string) (RetryPolicyProvider, error) {
	policy, ok := policies[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownRetryPolicy, name)
	}

	return policy, nil
}

// IsRetryPolicyKnown determines if the named policy exist
func IsRetryPolicyKnown(name string) bool {
	for _, p := range RetryPolicyNames() {
		if p == name {
			return true
		}
	}

	return false
}

// Duration is the period to sleep for try n, it includes a jitter
func (p RetryPolicy) Duration(n int) time.Duration {
	if n >= len(p.Intervals) {
		n = len(p.Intervals) - 1
	}

	delay := p.jitter(p.Intervals[n])
	if delay == 0 {
		delay = p.Intervals[0]
	}

	return delay
}

// RetrySleep sleeps for the duration for try n or until interrupted by ctx
func RetrySleep(ctx context.Context, p RetryPolicyProvider, n int) error {
	timer := time.NewTimer(p.Duration(n))

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	}
}

func linearPolicy(steps uint64, jitter float64, min time.Duration, max time.Duration) RetryPolicy {
	if max < min {
		max, min = min, max
	}

	p := RetryPolicy{
		Intervals: []time.Duration{},
		Jitter:    jitter,
	}

	stepSize := uint64(max-min) / steps
	for i := uint64(0); i < steps; i += 1 {
		p.Intervals = append(p.Intervals, time.Duration(uint64(min)+(i*stepSize)))
	}

	return p
}

func (p RetryPolicy) jitter(d time.Duration) time.Duration {
	if d == 0 {
		return 0
	}

	jf := (float64(d) * p.Jitter) + float64(rand.Int63n(int64(d)))

	return time.Duration(jf).Round(time.Millisecond)
}
