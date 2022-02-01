// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"math/rand"
	"time"
)

// RetryPolicy defines a period that failed jobs will be retried against
type RetryPolicy struct {
	// Intervals is a range of time periods backoff will be based off
	Intervals []time.Duration
	// Jitter is a factor applied to the specific interval avoid repeating same backoff periods
	Jitter float64
}

var (
	// RetryLinearTenMinutes is a 20-step policy between 1 and 10 minutes
	RetryLinearTenMinutes = linearPolicy(20, 0.90, time.Minute, 10*time.Minute)

	// RetryLinearOneHour is a 50-step policy between 10 minutes and 1 hour
	RetryLinearOneHour = linearPolicy(20, 0.90, 10*time.Minute, 60*time.Minute)

	// RetryLinearOneMinute is a 20-step policy between 1 second and 1 minute
	RetryLinearOneMinute = linearPolicy(20, 0.5, time.Second, time.Minute)

	// RetryDefault is the default retry policy
	RetryDefault = RetryLinearTenMinutes

	retryLinearTenSeconds = linearPolicy(20, 0.1, 500*time.Millisecond, 10*time.Second)
	retryForTesting       = linearPolicy(1, 0.1, time.Millisecond, 10*time.Millisecond)
)

// Duration is the period to sleep for try n, it includes a jitter
func (b RetryPolicy) Duration(n int) time.Duration {
	if n >= len(b.Intervals) {
		n = len(b.Intervals) - 1
	}

	delay := b.jitter(b.Intervals[n])
	if delay == 0 {
		delay = b.Intervals[0]
	}

	return delay
}

// Sleep attempts to sleep for the relevant duration for n, interruptable by ctx
func (b RetryPolicy) Sleep(ctx context.Context, n int) error {
	timer := time.NewTimer(b.Duration(n))

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

func (b RetryPolicy) jitter(d time.Duration) time.Duration {
	if d == 0 {
		return 0
	}

	jf := (float64(d) * b.Jitter) + float64(rand.Int63n(int64(d)))

	return time.Duration(jf).Round(time.Millisecond)
}
