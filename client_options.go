// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"fmt"
	"time"

	"github.com/nats-io/jsm.go/natscontext"
	"github.com/nats-io/nats.go"
)

// ClientOpts configures the client
type ClientOpts struct {
	concurrency   int
	replicas      int
	queues        map[string]*Queue
	taskRetention time.Duration
	retryPolicy   RetryPolicy
	memoryStore   bool
	statsPort     int
	logger        Logger
	nc            *nats.Conn
}

type ClientOpt func(opts *ClientOpts) error

// CustomLogger sets a custom logger to use for all logging
func CustomLogger(log Logger) ClientOpt {
	return func(opts *ClientOpts) error {
		opts.logger = log
		return nil
	}
}

// NatsConn sets an already connected NATS connection as communications channel
func NatsConn(nc *nats.Conn) ClientOpt {
	return func(opts *ClientOpts) error {
		opts.nc = nc
		return nil
	}
}

// PrometheusListenPort enables prometheus listening on a specific port
func PrometheusListenPort(port int) ClientOpt {
	return func(copts *ClientOpts) error {
		if port == 0 {
			return fmt.Errorf("port is required")
		}

		copts.statsPort = port
		return nil
	}
}

// NatsContext attempts to connect to the NATS client context c
func NatsContext(c string, opts ...nats.Option) ClientOpt {
	return func(copts *ClientOpts) error {
		nc, err := natscontext.Connect(c, opts...)
		if err != nil {
			return err
		}
		copts.nc = nc
		return nil
	}
}

// MemoryStorage enables storing tasks and work queue in memory in JetStream
func MemoryStorage() ClientOpt {
	return func(opts *ClientOpts) error {
		opts.memoryStore = true
		return nil
	}
}

// RetryBackoffPolicy uses p to schedule job retries, defaults to a linear curve backoff with jitter between 1 and 10 minutes
func RetryBackoffPolicy(p RetryPolicy) ClientOpt {
	return func(opts *ClientOpts) error {
		opts.retryPolicy = p
		return nil
	}
}

// ClientConcurrency sets the concurrency to use when executing tasks within this client for horizontal scaling.
// This is capped by the per-queue maximum concurrency set using the queue setting MaxConcurrent. Generally a
// queue would have a larger concurrency like 100 (DefaultQueueMaxConcurrent) and an individual task processor
// would be below that. This allows for horizontal and vertical scaling but without unbounded growth - the queue
// MaxConcurrent is the absolute upper limit for in-flight jobs for 1 specific queue.
func ClientConcurrency(c int) ClientOpt {
	return func(opts *ClientOpts) error {
		opts.concurrency = c
		return nil
	}
}

// StoreReplicas sets the replica level to keep for the tasks store and work queue
//
// Used only when initially creating the underlying streams.
func StoreReplicas(r uint) ClientOpt {
	return func(opts *ClientOpts) error {
		if r < 1 || r > 5 {
			return fmt.Errorf("replicas must be between 1 and 5")
		}

		opts.replicas = int(r)
		return nil
	}
}

// WorkQueues configures the client to consume messages from one or many queues
//
// When not set the "DEFAULT" queue will be used.
func WorkQueues(queues ...*Queue) ClientOpt {
	return func(opts *ClientOpts) error {
		for _, q := range queues {
			opts.queues[q.Name] = q
		}
		return nil
	}
}

// TaskRetention is the time tasks will be kept with.
//
// Used only when initially creating the underlying streams.
func TaskRetention(r time.Duration) ClientOpt {
	return func(opts *ClientOpts) error {
		opts.taskRetention = r
		return nil
	}
}
