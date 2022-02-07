// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"errors"
	"fmt"
)

var (
	// ErrTaskNotFound is the error indicating a task does not exist rather than a failure to load
	ErrTaskNotFound = errors.New("task not found")
	// ErrTerminateTask indicates that a task failed, and no further processing attempts should be made
	ErrTerminateTask = fmt.Errorf("terminate task")
	// ErrNoTasks indicates the task store is empty
	ErrNoTasks = fmt.Errorf("no tasks found")
	// ErrTaskPastDeadline indicates a task that was scheduled for handling is past its deadline
	ErrTaskPastDeadline = fmt.Errorf("past deadline")
	// ErrTaskAlreadyActive indicates that a task is already in the active state
	ErrTaskAlreadyActive = fmt.Errorf("task already active")
	// ErrTaskTypeCannotEnqueue indicates that a task is in a state where it cannot be enqueued as new
	ErrTaskTypeCannotEnqueue = fmt.Errorf("cannot enqueue a task in state")
	// ErrTaskUpdateFailed indicates a task update failed
	ErrTaskUpdateFailed = fmt.Errorf("failed updating task state")
	// ErrTaskAlreadyInState indicates an update failed because a task was already in the desired state
	ErrTaskAlreadyInState = fmt.Errorf("%w, already in desired state", ErrTaskUpdateFailed)
	// ErrTaskLoadFailed indicates a task failed for an unknown reason
	ErrTaskLoadFailed = fmt.Errorf("loading task failed")

	// ErrNoHandlerForTaskType indicates that a task could not be handled by any known handlers
	ErrNoHandlerForTaskType = fmt.Errorf("no handler for task type")
	// ErrDuplicateHandlerForTaskType indicates a task handler for a specific type is already registered
	ErrDuplicateHandlerForTaskType = fmt.Errorf("duplicate handler for task type")

	// ErrInvalidHeaders indicates that message headers from JetStream were not valid
	ErrInvalidHeaders = fmt.Errorf("coult not decode headers")
	// ErrContextWithoutDeadline indicates a context.Context was passed without deadline when it was expected
	ErrContextWithoutDeadline = fmt.Errorf("non deadline context given")
	// ErrInvalidStorageItem indicates a Work Queue item had no JetStream state associated with it
	ErrInvalidStorageItem = fmt.Errorf("invalid storage item")
	// ErrNoNatsConn indicates that a nil connection was supplied
	ErrNoNatsConn = fmt.Errorf("no NATS connection supplied")
	// ErrNoMux indicates that a processor was started with no routing mux configured
	ErrNoMux = fmt.Errorf("mux is required")

	// ErrQueueNotFound is the error indicating a queue does not exist rather than a failure to load
	ErrQueueNotFound = errors.New("queue not found")
	// ErrQueueConsumerNotFound indicates that the Work Queue store has no consumers defined
	ErrQueueConsumerNotFound = errors.New("queue consumer not found")
	// ErrQueueNameRequired indicates a queue has no name
	ErrQueueNameRequired = fmt.Errorf("queue name is required")
	// ErrQueueItemCorrupt indicates that an item received from the work queue was invalid - perhaps invalid JSON
	ErrQueueItemCorrupt = fmt.Errorf("corrupt queue item received")
	// ErrQueueItemInvalid is an item read from the queue with no data or obviously bad data
	ErrQueueItemInvalid = fmt.Errorf("invalid queue item received")
	// ErrInvalidQueueState indicates a queue was attempted to be used but no internal state is known of that queue
	ErrInvalidQueueState = fmt.Errorf("invalid queue storage state")
	// ErrDuplicateItem indicates that the Work Queue deduplication protection refused a message
	ErrDuplicateItem = fmt.Errorf("duplicate work queue item")

	// ErrUnknownEventType indicates that while parsing an event an unknown type of event was encountered
	ErrUnknownEventType = fmt.Errorf("unknown event type")
)