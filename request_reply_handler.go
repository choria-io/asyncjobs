// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	// RequestReplyContentTypeHeader is the header text sent to indicate the body encoding and type
	RequestReplyContentTypeHeader = "AJ-Content-Type"
	// RequestReplyDeadlineHeader is the header indicating the deadline for processing the item
	RequestReplyDeadlineHeader = "AJ-Handler-Deadline"
	// RequestReplyTerminateError is the header to send in a reply that the task should be terminated via ErrTerminateTask
	RequestReplyTerminateError = "AJ-Terminate"
	// RequestReplyError is the header indicating a generic failure in handling an item
	RequestReplyError = "AJ-Error"
	// RequestReplyTaskType is the content type indicating the payload is a Task in JSON format
	RequestReplyTaskType = "application/x-asyncjobs-task+json"
)

type requestReplyHandler struct {
	nc   *nats.Conn
	tt   string
	subj string
}

func newRequestReplyHandleFunc(nc *nats.Conn, tt string) HandlerFunc {
	h := &requestReplyHandler{
		nc: nc,
		tt: tt,
	}

	h.subj = RequestReplySubjectForTaskType(tt)

	return h.processTask
}

// RequestReplySubjectForTaskType returns the subject a request-reply handler should listen on for a specified task type
func RequestReplySubjectForTaskType(taskType string) string {
	if taskType == "" {
		return fmt.Sprintf(RequestReplyTaskHandlerPattern, "catchall")
	}
	return fmt.Sprintf(RequestReplyTaskHandlerPattern, taskType)
}

func (r *requestReplyHandler) processTask(ctx context.Context, logger Logger, task *Task) (any, error) {
	if r.nc == nil {
		return nil, fmt.Errorf("no connnection set")
	}

	var err error

	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, ErrRequestReplyNoDeadline
	}
	if time.Until(deadline) < 3*time.Second {
		return nil, ErrRequestReplyShortDeadline
	}

	msg := nats.NewMsg(r.subj)
	msg.Header.Add(RequestReplyContentTypeHeader, RequestReplyTaskType)
	msg.Header.Add(RequestReplyDeadlineHeader, deadline.Add(-2*time.Second).UTC().Format(time.RFC3339))
	msg.Data, err = json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("could not encode task: %v", err)
	}

	logger.Infof("Calling request-reply handler on %s", msg.Subject)
	res, err := r.nc.RequestMsgWithContext(ctx, msg)
	switch {
	case err == context.DeadlineExceeded:
		logger.Errorf("Request-Reply callout failed, no response received within %v", deadline)
		return nil, fmt.Errorf("%w: %v", ErrRequestReplyFailed, err)
	case err == nats.ErrNoResponders:
		logger.Errorf("Request-Reply handler failed, no responders on subject %s", msg.Subject)
		return nil, fmt.Errorf("%w: %v", ErrRequestReplyFailed, err)
	case err != nil:
		logger.Errorf("Request-Reply handler failed: %v", err)
		return nil, fmt.Errorf("%w: %v", ErrRequestReplyFailed, err)
	}

	if v := res.Header.Get(RequestReplyTerminateError); v != "" {
		return res.Data, fmt.Errorf("%s: %w", v, ErrTerminateTask)
	}

	if v := res.Header.Get(RequestReplyError); v != "" {
		return res.Data, errors.New(v)
	}

	return res.Data, nil
}
