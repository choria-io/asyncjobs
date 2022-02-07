// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Handler handles tasks
type Handler interface {
	// ProcessTask processes a single task, the response bytes will be stored in the original task
	ProcessTask(ctx context.Context, t *Task) (interface{}, error)
}

type entryHandler struct {
	ttype string
	hf    HandlerFunc
}

// HandlerFunc handles a single task, the response bytes will be stored in the original task
type HandlerFunc func(ctx context.Context, t *Task) (interface{}, error)

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

func notFoundHandler(_ context.Context, t *Task) (interface{}, error) {
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
