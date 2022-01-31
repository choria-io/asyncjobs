// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"fmt"
	"sync"
)

// Handler handles tasks
type Handler interface {
	// ProcessTask processes a single task, the response bytes will be stored in the original task
	ProcessTask(ctx context.Context, t *Task) (interface{}, error)
}

// HandlerFunc handles a single task, the response bytes will be stored in the original task
type HandlerFunc func(ctx context.Context, t *Task) (interface{}, error)

// Mux routes messages
//
// Note: this will change to be nearer to a server mux and include support for middleware
type Mux struct {
	hf map[string]HandlerFunc
	mu *sync.Mutex
}

// NewTaskRouter creates a new Mux
func NewTaskRouter() *Mux {
	return &Mux{
		hf: map[string]HandlerFunc{},
		mu: &sync.Mutex{},
	}
}

// Handler looks up the handler function for a task
func (m *Mux) Handler(t *Task) HandlerFunc {
	m.mu.Lock()
	defer m.mu.Unlock()

	hf, ok := m.hf[t.Type]
	if !ok {
		return func(ctx context.Context, t *Task) (interface{}, error) {
			return nil, fmt.Errorf("no handler for task type %s", t.Type)
		}
	}

	return hf
}

// HandleFunc registers a task for a taskType. The taskType must match exactly with the matching tasks
func (m *Mux) HandleFunc(taskType string, h HandlerFunc) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.hf[taskType]
	if ok {
		return fmt.Errorf("already have a handler for type %s tasks", taskType)
	}

	m.hf[taskType] = h

	return nil
}
