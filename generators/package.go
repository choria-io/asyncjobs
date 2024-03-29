// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package generators

// Generator is the interfaces generators must implement
type Generator interface {
	// RenderToDirectory renders the output to directory target
	RenderToDirectory(target string) error
}

// Package describe a configuration of a asyncjobs handler with multiple handlers loaded
type Package struct {
	// ContextName is the optional NATS Context name to use when none is configured
	ContextName string `yaml:"nats"`
	// WorkQueue is the optional Work Queue name to bind to, else DEFAULT will be used
	WorkQueue string `yaml:"queue"`
	// TaskHandlers is a list of handlers for tasks
	TaskHandlers []TaskHandler `yaml:"tasks"`
	// Name is an optional name for the generated go package
	Name string `yaml:"name"`
	// AJVersion is an optional version to use for the choria-io/asyncjobs dependency
	AJVersion string `yaml:"asyncjobs"`
	// RetryPolicy is the name of a retry policy, see RetryPolicyNames()
	RetryPolicy string `yaml:"retry"`
	// DiscardStates indicates what termination states to discard
	DiscardStates []string `yaml:"discard"`
	// TaskSignaturesOptional allows unsigned tasks to be used when AJ_VERIFICATION_KEY is set
	TaskSignaturesOptional bool `yaml:"task_signatures_optional"`
}

// TaskHandler is an individual Task Handler
type TaskHandler struct {
	// TaskType is the type to handle like email:new
	TaskType string `yaml:"type"`
	// Package is a golang package name that has a AsyncJobHandler() implementing HandlerFunc
	Package string `yaml:"package"`
	// Version is the version to fetch of this package
	Version string `yaml:"version"`
	// RequestReply indicates the handler is a callout to a remote service
	RequestReply bool `yaml:"remote"`
	// Command indicates the handler is a callout to a command in the given file
	Command string `yaml:"command"`
}
