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
	queue         *Queue
	taskRetention time.Duration
	retryPolicy   RetryPolicyProvider
	memoryStore   bool
	statsPort     int
	logger        Logger
	skipPrepare   bool
	discard       []TaskState

	nc *nats.Conn
}

// ClientOpt configures the client
type ClientOpt func(opts *ClientOpts) error

// DiscardTaskStates configures the client to discard Tasks that reach a final state in the list of supplied TaskState
func DiscardTaskStates(states ...TaskState) ClientOpt {
	return func(opts *ClientOpts) error {
		for _, s := range states {
			if s != TaskStateCompleted && s != TaskStateExpired && s != TaskStateTerminated {
				return fmt.Errorf("only states completed, expired or terminated can be discarded")
			}
		}

		opts.discard = append(opts.discard, states...)

		return nil
	}
}

// NoStorageInit skips setting up any queues or task stores when creating a client
func NoStorageInit() ClientOpt {
	return func(opts *ClientOpts) error {
		opts.skipPrepare = true
		return nil
	}
}

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
		if !nc.Opts.UseOldRequestStyle {
			return fmt.Errorf("connection with UseOldRequestStyle() is required")
		}

		opts.nc = nc
		return nil
	}
}

// PrometheusListenPort enables prometheus listening on a specific port
func PrometheusListenPort(port int) ClientOpt {
	return func(copts *ClientOpts) error {
		copts.statsPort = port
		return nil
	}
}

// NatsContext attempts to connect to the NATS client context c
func NatsContext(c string, opts ...nats.Option) ClientOpt {
	return func(copts *ClientOpts) error {
		nopts := []nats.Option{
			nats.MaxReconnects(-1),
			nats.CustomReconnectDelay(RetryLinearOneMinute.Duration),
			nats.UseOldRequestStyle(),
			nats.Name("Choria Asynchronous Jobs Client"),
			nats.ReconnectHandler(func(nc *nats.Conn) {
				copts.logger.Infof("Reconnected to NATS server %s", nc.ConnectedUrl())
			}),
			nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
				copts.logger.Errorf("Disconnected from server: %v", err)
			}),
			nats.ErrorHandler(func(nc *nats.Conn, _ *nats.Subscription, err error) {
				url := nc.ConnectedUrl()
				if url == "" {
					copts.logger.Errorf("Unexpected NATS error: %s", err)
				} else {
					copts.logger.Errorf("Unexpected NATS error from server %s: %s", url, err)
				}
			}),
			nats.CustomReconnectDelay(func(n int) time.Duration {
				d := RetryLinearOneMinute.Duration(n)
				copts.logger.Warnf("Sleeping %v till the next reconnection attempt after %d attempts", d, n)

				return d
			}),
		}

		nc, err := natscontext.Connect(c, append(nopts, opts...)...)
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
func RetryBackoffPolicy(p RetryPolicyProvider) ClientOpt {
	return func(opts *ClientOpts) error {
		opts.retryPolicy = p
		return nil
	}
}

// RetryBackoffPolicyName uses the policy named to schedule job retries by using RetryPolicyLookup(name)
func RetryBackoffPolicyName(name string) ClientOpt {
	return func(opts *ClientOpts) error {
		p, err := RetryPolicyLookup(name)
		if err != nil {
			return err
		}

		return RetryBackoffPolicy(p)(opts)
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

// WorkQueue configures the client to consume messages from a specific queue
//
// When not set the "DEFAULT" queue will be used.
func WorkQueue(queue *Queue) ClientOpt {
	return func(opts *ClientOpts) error {
		if opts.queue != nil {
			return fmt.Errorf("a queue has already been defined")
		}

		opts.queue = queue

		return nil
	}
}

// BindWorkQueue binds the client to a work queue that should already exist
func BindWorkQueue(queue string) ClientOpt {
	return func(opts *ClientOpts) error {
		if queue == "" {
			return fmt.Errorf("a queue name is required")
		}
		if opts.queue != nil {
			return fmt.Errorf("a queue has already been defined")
		}

		opts.queue = &Queue{Name: queue, NoCreate: true}

		return nil
	}
}

// TaskRetention is the time tasks will be kept for in the task storage
//
// Used only when initially creating the underlying streams.
func TaskRetention(r time.Duration) ClientOpt {
	return func(opts *ClientOpts) error {
		opts.taskRetention = r
		return nil
	}
}
