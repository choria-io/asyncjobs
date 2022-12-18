// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
)

type entryHandler struct {
	ttype string
	hf    HandlerFunc
}

// HandlerFunc handles a single task, the response bytes will be stored in the original task
type HandlerFunc func(ctx context.Context, log Logger, t *Task) (any, error)

// Mux routes messages
//
// Note: this will change to be nearer to a server mux and include support for middleware
type Mux struct {
	hf  map[string]*entryHandler
	ehf []*entryHandler
	mu  *sync.Mutex
}

// NewTaskRouter creates a new Mux
func NewTaskRouter() *Mux {
	return &Mux{
		hf:  map[string]*entryHandler{},
		ehf: []*entryHandler{},
		mu:  &sync.Mutex{},
	}
}

func notFoundHandler(_ context.Context, _ Logger, t *Task) (any, error) {
	return nil, fmt.Errorf("%w %q", ErrNoHandlerForTaskType, t.Type)
}

// Handler looks up the handler function for a task
func (m *Mux) Handler(t *Task) HandlerFunc {
	m.mu.Lock()
	defer m.mu.Unlock()

	hf, ok := m.hf[t.Type]
	if ok {
		return hf.hf
	}

	for _, hf := range m.ehf {
		if strings.HasPrefix(t.Type, hf.ttype) {
			return hf.hf
		}
	}

	return notFoundHandler
}

// HandleFunc registers a task for a taskType. The taskType must match exactly with the matching tasks
func (m *Mux) HandleFunc(taskType string, h HandlerFunc) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.hf[taskType]
	if ok {
		return fmt.Errorf("%w %q", ErrDuplicateHandlerForTaskType, taskType)
	}

	m.hf[taskType] = &entryHandler{hf: h, ttype: taskType}
	m.ehf = append(m.ehf, m.hf[taskType])

	sort.Slice(m.ehf, func(i, j int) bool {
		return len(m.ehf[i].ttype) > len(m.ehf[j].ttype)
	})

	return nil
}

// RequestReply sets up a delegated handler via NATS Request-Reply
func (m *Mux) RequestReply(taskType string, client *Client) error {
	h := newRequestReplyHandleFunc(client.opts.nc, taskType)
	return m.HandleFunc(taskType, h)
}

// ExternalProcess sets up a delegated handler that calls an external command to handle the task.
//
// The task will be passed in JSON format on STDIN, any STDOUT/STDERR output will become the task
// result. Any non 0 exit code will be treated as a task failure.
func (m *Mux) ExternalProcess(taskType string, command string) error {
	return m.HandleFunc(taskType, func(ctx context.Context, log Logger, task *Task) (any, error) {
		stat, err := os.Stat(command)
		if err != nil || stat.IsDir() {
			return nil, ErrExternalCommandNotFound
		}

		tj, err := json.Marshal(task)
		if err != nil {
			return nil, err
		}

		stdinFile, err := os.CreateTemp("", "asyncjobs-task")
		if err != nil {
			return nil, err
		}
		defer os.Remove(stdinFile.Name())
		defer stdinFile.Close()

		_, err = stdinFile.Write(tj)
		if err != nil {
			return nil, err
		}
		stdinFile.Close()

		start := time.Now()
		log.Infof("Running task %s try %d using %q", task.ID, task.Tries, command)

		cmd := exec.CommandContext(ctx, command)
		cmd.Env = append(cmd.Env, fmt.Sprintf("CHORIA_AJ_TASK=%s", stdinFile.Name()))
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Errorf("Running %s failed: %q", command, out)
			return nil, fmt.Errorf("%w: %v", ErrExternalCommandFailed, err)
		}

		log.Infof("Task %s completed using %q after %s and %d tries with %s payload", task.ID, command, time.Since(start), task.Tries, humanize.IBytes(uint64(len(out))))

		return string(out), nil
	})
}
