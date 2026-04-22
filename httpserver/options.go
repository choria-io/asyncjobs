// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"fmt"
	"time"

	"github.com/choria-io/asyncjobs"
)

const (
	defaultBindAddress  = "127.0.0.1:8080"
	defaultReadHeader   = 5 * time.Second
	defaultReadTimeout  = 30 * time.Second
	defaultWriteTimeout = 30 * time.Second
	defaultIdleTimeout  = 60 * time.Second
	defaultMaxBodyBytes = 512 * 1024
)

// Options configures the HTTP server. Construct via NewServer — callers should
// not zero-initialize this struct directly; pass Option values instead.
type Options struct {
	bindAddress                  string
	tlsCertFile                  string
	tlsKeyFile                   string
	clientCAFile                 string
	logger                       asyncjobs.Logger
	readHeaderTimeout            time.Duration
	readTimeout                  time.Duration
	writeTimeout                 time.Duration
	idleTimeout                  time.Duration
	maxBodyBytes                 int64
	allowCreateDefaultQueue      bool
	allowUnauthenticatedExposure bool
}

// Option configures a Server.
type Option func(*Options) error

func defaultOptions() *Options {
	return &Options{
		bindAddress:       defaultBindAddress,
		readHeaderTimeout: defaultReadHeader,
		readTimeout:       defaultReadTimeout,
		writeTimeout:      defaultWriteTimeout,
		idleTimeout:       defaultIdleTimeout,
		maxBodyBytes:      defaultMaxBodyBytes,
	}
}

// WithBindAddress sets the listen address in host:port form. Defaults to
// loopback ("127.0.0.1:8080"). Non-loopback values require either
// WithUnauthenticatedExposure (the deployer has fronted the server with a
// reverse proxy) or WithClientCA (mTLS authenticates every client).
func WithBindAddress(addr string) Option {
	return func(o *Options) error {
		if addr == "" {
			return fmt.Errorf("bind address cannot be empty")
		}
		o.bindAddress = addr
		return nil
	}
}

// WithTLS enables TLS on the listener. Both cert and key must be provided.
// TLS alone provides transport encryption; it does not authenticate callers.
// Pair with WithClientCA to require verified client certificates.
func WithTLS(certFile, keyFile string) Option {
	return func(o *Options) error {
		if certFile == "" || keyFile == "" {
			return fmt.Errorf("both TLS cert and key files are required")
		}
		o.tlsCertFile = certFile
		o.tlsKeyFile = keyFile
		return nil
	}
}

// WithClientCA enables mTLS: every TLS client must present a certificate
// chain that verifies against the supplied CA bundle. Requires WithTLS.
// The server performs no authorization beyond "the client presented a cert
// that this CA vouches for" — certificate subjects are not mapped to scopes.
func WithClientCA(caFile string) Option {
	return func(o *Options) error {
		if caFile == "" {
			return fmt.Errorf("client CA file path cannot be empty")
		}
		o.clientCAFile = caFile
		return nil
	}
}

// WithLogger installs a Logger. If unset, a no-op logger is used.
func WithLogger(l asyncjobs.Logger) Option {
	return func(o *Options) error {
		if l == nil {
			return fmt.Errorf("logger cannot be nil")
		}
		o.logger = l
		return nil
	}
}

// WithReadTimeout overrides the http.Server ReadTimeout.
func WithReadTimeout(d time.Duration) Option {
	return func(o *Options) error {
		if d <= 0 {
			return fmt.Errorf("read timeout must be positive")
		}
		o.readTimeout = d
		return nil
	}
}

// WithWriteTimeout overrides the http.Server WriteTimeout.
func WithWriteTimeout(d time.Duration) Option {
	return func(o *Options) error {
		if d <= 0 {
			return fmt.Errorf("write timeout must be positive")
		}
		o.writeTimeout = d
		return nil
	}
}

// WithMaxBodyBytes overrides the per-request body size cap. Bodies exceeding
// this value yield HTTP 413.
func WithMaxBodyBytes(n int64) Option {
	return func(o *Options) error {
		if n <= 0 {
			return fmt.Errorf("max body bytes must be positive")
		}
		o.maxBodyBytes = n
		return nil
	}
}

// WithAllowCreateDefaultQueue permits callers to use the library's implicit
// DEFAULT queue. Off by default so production deployments must declare queues
// explicitly.
func WithAllowCreateDefaultQueue() Option {
	return func(o *Options) error {
		o.allowCreateDefaultQueue = true
		return nil
	}
}

// WithUnauthenticatedExposure acknowledges that the server will be bound to a
// non-loopback address with no built-in authentication. The deployer is
// expected to front the server with a reverse proxy that terminates authn.
// Without this option (or WithClientCA), non-loopback binds are refused at
// construction time.
func WithUnauthenticatedExposure() Option {
	return func(o *Options) error {
		o.allowUnauthenticatedExposure = true
		return nil
	}
}
