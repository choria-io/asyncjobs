// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"log"
)

// Logger is a pluggable logger interface
type Logger interface {
	Debugf(format string, v ...interface{})
	Infof(format string, v ...interface{})
	Warnf(format string, v ...interface{})
	Errorf(format string, v ...interface{})
}

// Default console logger
type defaultLogger struct{}

func (l *defaultLogger) Infof(format string, v ...interface{}) {
	log.Printf(format, v...)
}

func (l *defaultLogger) Warnf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

func (l *defaultLogger) Errorf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

func (l *defaultLogger) Debugf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

// Logger placeholder
type noopLogger struct{}

func (l *noopLogger) Infof(format string, v ...interface{})  {}
func (l *noopLogger) Warnf(format string, v ...interface{})  {}
func (l *noopLogger) Errorf(format string, v ...interface{}) {}
func (l *noopLogger) Debugf(format string, v ...interface{}) {}
