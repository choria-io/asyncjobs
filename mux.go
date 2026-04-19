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
	ttype    string
	hf       HandlerFunc
	mws      []Middleware
	combined HandlerFunc
}

// HandlerFunc handles a single task, the response bytes will be stored in the original task
type HandlerFunc func(ctx context.Context, log Logger, t *Task) (any, error)

// Middleware wraps a HandlerFunc to add cross-cutting behavior like logging,
// metrics, tracing, authentication, or panic recovery.
//
// A middleware should normally invoke next(ctx, log, t) and return its result
// and error unchanged unless deliberately transforming them. To short-circuit
// (for example on an authentication failure) return without calling next.
//
// Middleware must use the (ctx, log, t) arguments passed to the returned
// closure. Capturing values from the surrounding scope at construction time
// will leak them across dispatches.
//
// A typical pass-through middleware looks like:
//
//	func Logging(next HandlerFunc) HandlerFunc {
//	    return func(ctx context.Context, log Logger, t *Task) (any, error) {
//	        start := time.Now()
//	        res, err := next(ctx, log, t)
//	        log.Infof("task %s %s took %s err=%v", t.Type, t.ID, time.Since(start), err)
//	        return res, err
//	    }
//	}
type Middleware func(HandlerFunc) HandlerFunc

// Chain composes middleware into a single Middleware preserving order, so
// Chain(a, b, c) wraps a handler such that a runs outermost, then b, then c.
// It is useful for building reusable bundles like
// auth+logging+metrics that can be passed to Use or HandleFunc as one value.
func Chain(mws ...Middleware) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		for i := len(mws) - 1; i >= 0; i-- {
			next = mws[i](next)
		}
		return next
	}
}

// Mux routes tasks to handlers and supports global and per-route middleware.
type Mux struct {
	hf  map[string]*entryHandler
	ehf []*entryHandler
	mws []Middleware
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

// Handler looks up the handler function for a task. The returned function has
// any registered global and per-route middleware already applied. Tasks with
// no matching handler resolve to a built-in handler that returns
// ErrNoHandlerForTaskType; that handler is intentionally not wrapped by
// middleware so unrouted tasks do not generate logging or metric noise.
func (m *Mux) Handler(t *Task) HandlerFunc {
	m.mu.Lock()
	defer m.mu.Unlock()

	if e, ok := m.hf[t.Type]; ok {
		return e.combined
	}

	for _, e := range m.ehf {
		if strings.HasPrefix(t.Type, e.ttype) {
			return e.combined
		}
	}

	return notFoundHandler
}

// Use appends middleware that will be applied to every registered handler.
// Middleware registered earlier runs outermost: Use(Recovery, Logging) yields
// a chain like Recovery(Logging(Per-route(handler))) on dispatch. Middleware
// registered via Use always wraps any per-route middleware passed to
// HandleFunc.
//
// Use may be called before or after HandleFunc; existing handlers are
// rewrapped so subsequent dispatches see the new chain. Dispatches already in
// flight keep the chain they previously resolved.
//
// Returns ErrInvalidMiddleware if any of mws is nil. The built-in handler
// returned for unrouted task types is not wrapped.
func (m *Mux) Use(mws ...Middleware) error {
	for _, mw := range mws {
		if mw == nil {
			return ErrInvalidMiddleware
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.mws = append(m.mws, mws...)
	for _, e := range m.hf {
		e.combined = m.buildChain(e)
	}

	return nil
}

// HandleFunc registers h for an exact taskType match. Optional per-route
// middleware in mws is applied inside any middleware registered via Use, in
// the order given (first-registered runs outermost). Returns
// ErrDuplicateHandlerForTaskType if taskType is already registered, or
// ErrInvalidMiddleware if any of mws is nil.
func (m *Mux) HandleFunc(taskType string, h HandlerFunc, mws ...Middleware) error {
	for _, mw := range mws {
		if mw == nil {
			return ErrInvalidMiddleware
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.hf[taskType]
	if ok {
		return fmt.Errorf("%w %q", ErrDuplicateHandlerForTaskType, taskType)
	}

	e := &entryHandler{hf: h, ttype: taskType, mws: mws}
	e.combined = m.buildChain(e)

	m.hf[taskType] = e
	m.ehf = append(m.ehf, e)

	sort.Slice(m.ehf, func(i, j int) bool {
		return len(m.ehf[i].ttype) > len(m.ehf[j].ttype)
	})

	return nil
}

// buildChain wraps the entry's base handler with its per-route middleware
// and then with the Mux-level middleware. Must be called with m.mu held.
func (m *Mux) buildChain(e *entryHandler) HandlerFunc {
	h := e.hf
	for i := len(e.mws) - 1; i >= 0; i-- {
		h = e.mws[i](h)
	}
	for i := len(m.mws) - 1; i >= 0; i-- {
		h = m.mws[i](h)
	}
	return h
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
