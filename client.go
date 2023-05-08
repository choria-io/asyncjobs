// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Client connects Task producers and Task handlers to the backend
type Client struct {
	opts    *ClientOpts
	storage Storage

	log Logger
}

// NewClient creates a new client, one of NatsConn() or NatsContext() must be passed, other options are optional.
//
// When no Queue() is supplied a default queue called DEFAULT will be used
func NewClient(opts ...ClientOpt) (*Client, error) {
	copts := &ClientOpts{
		replicas:    1,
		concurrency: 10,
		retryPolicy: RetryDefault,
		logger:      &noopLogger{},
	}

	var err error
	for _, opt := range opts {
		err = opt(copts)
		if err != nil {
			return nil, err
		}
	}

	err = copts.validate()
	if err != nil {
		return nil, err
	}

	c := &Client{opts: copts, log: copts.logger}
	c.storage, err = newJetStreamStorage(copts.nc, copts.retryPolicy, c.log)
	if err != nil {
		return nil, err
	}

	if c.opts.queue == nil {
		c.opts.queue = newDefaultQueue()
		c.log.Debugf("Creating %s queue with no user defined queues set", c.opts.queue.Name)
	}

	if !copts.skipPrepare {
		err = c.setupStreams()
		if err != nil {
			return nil, err
		}

		err = c.setupQueues()
		if err != nil {
			return nil, err
		}
	}

	return c, nil
}

// Run starts processing messages using the router until error or interruption
func (c *Client) Run(ctx context.Context, router *Mux) error {
	if c.opts.queue == nil {
		return fmt.Errorf("no queue defined")
	}

	proc, err := newProcessor(c)
	if err != nil {
		return err
	}

	c.startPrometheus()

	return proc.processMessages(ctx, router)
}

// LoadTaskByID loads a task from the backend using its ID
func (c *Client) LoadTaskByID(id string) (*Task, error) {
	task, err := c.storage.LoadTaskByID(id)
	if err != nil {
		return nil, err
	}

	err = c.verifyTaskSignature(task)
	if err != nil {
		return nil, err
	}

	return task, nil
}

// RetryTaskByID will retry a task, first removing an entry from the Work Queue if already there
func (c *Client) RetryTaskByID(ctx context.Context, id string) error {
	return c.opts.queue.retryTaskByID(ctx, id)
}

// EnqueueTask adds a task to the named queue which must already exist
func (c *Client) EnqueueTask(ctx context.Context, task *Task) error {
	task.Queue = c.opts.queue.Name

	err := c.signTask(task)
	if err != nil {
		return err
	}

	return c.opts.queue.enqueueTask(ctx, task)
}

func (c *Client) verifyTaskSignature(task *Task) error {
	// is disabled
	if c.opts.publicKey == nil && c.opts.publicKeyFile == "" {
		return nil
	}

	switch {
	case !c.opts.optionalTaskSignatures && task.Signature == "":
		return ErrTaskNotSigned

	case task.Signature == "":
		return nil
	}

	var pubKey ed25519.PublicKey

	switch {
	case c.opts.publicKey != nil:
		pubKey = c.opts.publicKey

	case c.opts.publicKeyFile != "":
		kf, err := os.ReadFile(c.opts.publicKeyFile)
		if err != nil {
			return err
		}

		kb, err := hex.DecodeString(string(kf))
		if err != nil {
			return err
		}

		if len(kb) != ed25519.PublicKeySize {
			return fmt.Errorf("invalid public key")
		}

		pubKey = kb

	case c.opts.seedFile != "":
		sf, err := os.ReadFile(c.opts.seedFile)
		if err != nil {
			return err
		}

		sb, err := hex.DecodeString(string(sf))
		if err != nil {
			return err
		}

		if len(sb) != ed25519.SeedSize {
			return fmt.Errorf("invalid seed length")
		}

		pk := ed25519.NewKeyFromSeed(sb)
		defer func() {
			io.ReadFull(rand.Reader, pk[:])
			io.ReadFull(rand.Reader, sb[:])
			io.ReadFull(rand.Reader, sf[:])
		}()

		pubKey = pk.Public().(ed25519.PublicKey)

	default:
		if !c.opts.optionalTaskSignatures {
			return fmt.Errorf("no task verification keys configured")
		}

		return nil
	}

	msg, err := task.signatureMessage()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTaskSignatureInvalid, err)
	}

	sig, err := hex.DecodeString(task.Signature)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTaskSignatureInvalid, err)
	}

	if !ed25519.Verify(pubKey, msg, sig) {
		return ErrTaskSignatureInvalid
	}

	return nil
}

func (c *Client) signTask(task *Task) error {
	switch {
	case c.opts.privateKey != nil:
		return task.sign(c.opts.privateKey)

	case c.opts.seedFile != "":
		sf, err := os.ReadFile(c.opts.seedFile)
		if err != nil {
			return err
		}

		sb, err := hex.DecodeString(string(sf))
		if err != nil {
			return err
		}

		if len(sb) != ed25519.SeedSize {
			return fmt.Errorf("invalid seed length")
		}

		pk := ed25519.NewKeyFromSeed(sb)
		defer func() {
			io.ReadFull(rand.Reader, pk[:])
			io.ReadFull(rand.Reader, sb[:])
			io.ReadFull(rand.Reader, sf[:])
		}()

		return task.sign(pk)
	}

	return nil
}

// StorageAdmin access admin features of the storage backend
func (c *Client) StorageAdmin() StorageAdmin {
	return c.storage.(*jetStreamStorage)
}

// ScheduledTasksStorage gives access to administrative functions for task maintenance
func (c *Client) ScheduledTasksStorage() ScheduledTaskStorage {
	return c.storage.(*jetStreamStorage)
}

// NewScheduledTask creates a new scheduled task, an existing schedule will result in failure
func (c *Client) NewScheduledTask(name string, schedule string, queue string, task *Task) error {
	st, _, err := newScheduledTaskFromTask(name, schedule, queue, task)
	if err != nil {
		return err
	}

	return c.storage.SaveScheduledTask(st, false)
}

// RemoveScheduledTask removes a scheduled task
func (c *Client) RemoveScheduledTask(name string) error {
	return c.storage.DeleteScheduledTaskByName(name)
}

// LoadScheduledTaskByName loads a scheduled task by name
func (c *Client) LoadScheduledTaskByName(name string) (*ScheduledTask, error) {
	return c.storage.LoadScheduledTaskByName(name)
}

func (c *Client) startPrometheus() {
	if c.opts.statsPort == 0 {
		return
	}

	c.log.Warnf("Exposing Prometheus metrics on port %d", c.opts.statsPort)
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		err := http.ListenAndServe(fmt.Sprintf(":%d", c.opts.statsPort), nil)
		if err != nil {
			c.log.Errorf("Could not start Prometheus listener on %d: %s", c.opts.statsPort, err)
		}
	}()
}

func (c *Client) setupStreams() error {
	err := c.storage.PrepareTasks(c.opts.memoryStore, c.opts.replicas, c.opts.taskRetention)
	if err != nil {
		return err
	}

	return c.storage.PrepareConfigurationStore(c.opts.memoryStore, c.opts.replicas)
}

func nowPointer() *time.Time {
	t := time.Now().UTC()
	return &t
}

func (c *Client) setTaskActive(ctx context.Context, t *Task) error {
	t.State = TaskStateActive
	t.LastTriedAt = nowPointer()
	t.LastErr = ""

	return c.storage.SaveTaskState(ctx, t, true)
}

func (c *Client) shouldDiscardTask(t *Task) bool {
	for _, state := range c.opts.discard {
		if t.State == state {
			return true
		}
	}

	return false
}

func (c *Client) saveOrDiscardTaskIfDesired(ctx context.Context, t *Task) error {
	if !c.shouldDiscardTask(t) {
		return c.storage.SaveTaskState(ctx, t, true)
	}

	c.storage.PublishTaskStateChangeEvent(ctx, t)

	c.log.Debugf("Discarding task with state %s based on desired discards %q", t.State, c.opts.discard)
	return c.storage.DeleteTaskByID(t.ID)
}

func (c *Client) setTaskSuccess(ctx context.Context, t *Task, payload any) error {
	t.LastTriedAt = nowPointer()
	t.State = TaskStateCompleted
	t.LastErr = ""

	t.Result = &TaskResult{
		Payload:     payload,
		CompletedAt: time.Now().UTC(),
	}

	return c.saveOrDiscardTaskIfDesired(ctx, t)
}

func (c *Client) handleTaskTerminated(ctx context.Context, t *Task, terr error) error {
	t.LastErr = terr.Error()
	t.LastTriedAt = nowPointer()
	t.State = TaskStateTerminated

	return c.saveOrDiscardTaskIfDesired(ctx, t)
}

func (c *Client) handleTaskExpired(ctx context.Context, t *Task) error {
	t.State = TaskStateExpired

	return c.saveOrDiscardTaskIfDesired(ctx, t)
}

func (c *Client) handleTaskError(ctx context.Context, t *Task, terr error) error {
	t.LastErr = terr.Error()
	t.LastTriedAt = nowPointer()
	t.State = TaskStateRetry

	if errors.Is(terr, ErrTaskDependenciesFailed) {
		t.State = TaskStateUnreachable
	} else if t.Queue != "" && t.Queue == c.opts.queue.Name {
		if c.opts.queue.MaxTries == t.Tries {
			c.log.Infof("Expiring task %s after %d / %d tries", t.ID, t.Tries, c.opts.queue.MaxTries)
			t.State = TaskStateExpired
		}
	}

	return c.saveOrDiscardTaskIfDesired(ctx, t)
}

func (c *Client) setupQueues() error {
	c.opts.queue.storage = c.storage
	return c.storage.PrepareQueue(c.opts.queue, c.opts.replicas, c.opts.memoryStore)
}
