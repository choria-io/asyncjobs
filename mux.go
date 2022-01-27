// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"fmt"
	"sync"
)

type Handler interface {
	ProcessTask(ctx context.Context, t *Task) ([]byte, error)
}

type HandlerFunc func(ctx context.Context, t *Task) ([]byte, error)

type Mux struct {
	hf map[string]HandlerFunc
	mu sync.Mutex
}

func NewTaskRouter() *Mux {
	return &Mux{
		hf: map[string]HandlerFunc{},
	}
}

func (m *Mux) Handler(t *Task) HandlerFunc {
	m.mu.Lock()
	defer m.mu.Unlock()

	hf, ok := m.hf[t.Type]
	if !ok {
		return func(ctx context.Context, t *Task) ([]byte, error) {
			return nil, fmt.Errorf("no handler for task type %s", t.Type)
		}
	}

	return hf
}

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
func (m *Mux) Handle(taskType string, h Handler) error {
	return m.HandleFunc(taskType, h.ProcessTask)
}
