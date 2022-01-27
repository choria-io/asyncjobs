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
	Noticef(format string, v ...interface{})
	Warnf(format string, v ...interface{})
	Errorf(format string, v ...interface{})
}

type defaultLogger struct{}

func (l *defaultLogger) Noticef(format string, v ...interface{}) {
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
