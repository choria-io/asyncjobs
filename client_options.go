package jsaj

import (
	"fmt"
	"time"
)

type ClientOpts struct {
	concurrency   int
	replicas      int
	queues        map[string]*Queue
	taskRetention time.Duration
}

type ClientOpt func(opts *ClientOpts) error

func ClientConcurrency(c int) ClientOpt {
	return func(opts *ClientOpts) error {
		opts.concurrency = c
		return nil
	}
}

func TaskStoreReplicas(r uint) ClientOpt {
	return func(opts *ClientOpts) error {
		if r < 1 || r > 5 {
			return fmt.Errorf("replicas must be between 1 and 5")
		}

		opts.replicas = int(r)
		return nil
	}
}

func WorkQueues(queues ...Queue) ClientOpt {
	return func(opts *ClientOpts) error {
		for _, q := range queues {
			opts.queues[q.Name] = &q
		}
		return nil
	}
}

func TaskRetention(r time.Duration) ClientOpt {
	return func(opts *ClientOpts) error {
		opts.taskRetention = r
		return nil
	}
}
