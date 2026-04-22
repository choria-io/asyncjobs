// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"net/http"
	"runtime/debug"
	"time"

	"github.com/choria-io/asyncjobs"
	"github.com/choria-io/asyncjobs/httpserver/internal/gen"
)

// recoveryMiddleware converts panics into HTTP 500 responses. The stack trace
// is logged; nothing about it is returned to the client.
func recoveryMiddleware(log asyncjobs.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				log.Errorf("panic handling %s %s: %v\n%s", r.Method, r.URL.Path, rec, debug.Stack())
				writeError(w, http.StatusInternalServerError, gen.ErrorErrorCodeInternal, "internal server error", "panic", "")
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// loggingMiddleware records method, path, status, and duration. It never
// emits request bodies, query strings with potentially sensitive data, or
// the Authorization header.
func loggingMiddleware(log asyncjobs.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			lrw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(lrw, r)
			log.Infof("%s %s %d %s", r.Method, r.URL.Path, lrw.status, time.Since(start))
		})
	}
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

// bodyLimitMiddleware wraps the request body in http.MaxBytesReader. It is
// installed on the top-level mux so it applies to every request, including
// endpoints that do not parse a body.
func bodyLimitMiddleware(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
