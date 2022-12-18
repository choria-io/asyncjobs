// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"log"
)

// Logger is a pluggable logger interface
type Logger interface {
	Debugf(format string, v ...any)
	Infof(format string, v ...any)
	Warnf(format string, v ...any)
	Errorf(format string, v ...any)
}

// Default console logger
type defaultLogger struct{}

func (l *defaultLogger) Infof(format string, v ...any) {
	log.Printf(format, v...)
}

func (l *defaultLogger) Warnf(format string, v ...any) {
	log.Printf(format, v...)
}

func (l *defaultLogger) Errorf(format string, v ...any) {
	log.Printf(format, v...)
}

func (l *defaultLogger) Debugf(format string, v ...any) {
	log.Printf(format, v...)
}

// Logger placeholder
type noopLogger struct{}

func (l *noopLogger) Infof(format string, v ...any)  {}
func (l *noopLogger) Warnf(format string, v ...any)  {}
func (l *noopLogger) Errorf(format string, v ...any) {}
func (l *noopLogger) Debugf(format string, v ...any) {}
