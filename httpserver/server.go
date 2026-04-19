// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/choria-io/asyncjobs"
	"github.com/choria-io/asyncjobs/httpserver/internal/gen"
)

// Server is the asyncjobs HTTP API surface. It implements gen.ServerInterface
// and binds to a dedicated http.ServeMux. The server performs no
// authentication and no authorization; operators front it with a reverse
// proxy or configure mTLS via WithClientCA. Construct via NewServer.
type Server struct {
	client      *asyncjobs.Client
	opts        *Options
	log         asyncjobs.Logger
	mux         *http.ServeMux
	handler     http.Handler
	openAPIJSON []byte
	httpServer  *http.Server
	tlsConfig   *tls.Config
	idempotency *idempotencyStore
	listSem     *listSemaphore
}

// NewServer wires a new HTTP server against the supplied client. Provided
// Options are validated eagerly so configuration errors surface at
// construction time, not at first request.
func NewServer(client *asyncjobs.Client, opts ...Option) (*Server, error) {
	if client == nil {
		return nil, fmt.Errorf("client is required")
	}

	o := defaultOptions()
	for _, opt := range opts {
		if err := opt(o); err != nil {
			return nil, err
		}
	}

	if o.logger == nil {
		o.logger = &discardLogger{}
	}

	if err := validateBindAcknowledgement(o); err != nil {
		return nil, err
	}

	tlsCfg, err := buildTLSConfig(o)
	if err != nil {
		return nil, err
	}

	s := &Server{
		client:      client,
		opts:        o,
		log:         o.logger,
		mux:         http.NewServeMux(),
		tlsConfig:   tlsCfg,
		idempotency: newIdempotencyStore(),
		listSem:     newListSemaphore(1),
	}

	specJSON, err := yamlToJSON(gen.OpenAPIYAML)
	if err != nil {
		return nil, fmt.Errorf("converting embedded OpenAPI spec: %w", err)
	}
	s.openAPIJSON = specJSON

	s.mux.HandleFunc("GET /v1/openapi.json", s.handleOpenAPISpec)

	gen.HandlerWithOptions(s, gen.StdHTTPServerOptions{
		BaseRouter: s.mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeError(w, http.StatusBadRequest, gen.ErrorErrorCodeInvalidArgument, err.Error(), "", "")
		},
	})

	s.handler = chain(s.mux,
		recoveryMiddleware(s.log),
		loggingMiddleware(s.log),
		bodyLimitMiddleware(o.maxBodyBytes),
	)

	return s, nil
}

// Handler returns the assembled http.Handler. Primarily for tests; Run should
// be used in production.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Run starts the HTTP server and blocks until ctx is canceled or the server
// exits on its own.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.opts.bindAddress)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.opts.bindAddress, err)
	}

	s.httpServer = &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: s.opts.readHeaderTimeout,
		ReadTimeout:       s.opts.readTimeout,
		WriteTimeout:      s.opts.writeTimeout,
		IdleTimeout:       s.opts.idleTimeout,
		TLSConfig:         s.tlsConfig,
	}

	errCh := make(chan error, 1)
	go func() {
		if s.opts.tlsCertFile != "" {
			errCh <- s.httpServer.ServeTLS(ln, s.opts.tlsCertFile, s.opts.tlsKeyFile)
			return
		}
		errCh <- s.httpServer.Serve(ln)
	}()

	s.log.Infof("asyncjobs HTTP API listening on %s (%s)", s.opts.bindAddress, s.authMode())

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.opts.writeTimeout)
		defer cancel()
		err := s.httpServer.Shutdown(shutdownCtx)
		<-errCh
		return err
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// authMode returns a short label describing how the server authenticates
// callers. Used in startup logs and the /v1/info response.
func (s *Server) authMode() gen.InfoAuth {
	if s.opts.clientCAFile != "" {
		return gen.Mtls
	}
	return gen.None
}

// validateBindAcknowledgement enforces that a non-loopback bind is either
// fronted by a proxy (acknowledged via WithUnauthenticatedExposure) or
// authenticated by mTLS (via WithClientCA).
func validateBindAcknowledgement(o *Options) error {
	loopback, err := isLoopbackBind(o.bindAddress)
	if err != nil {
		return err
	}
	if loopback {
		return nil
	}
	if o.allowUnauthenticatedExposure || o.clientCAFile != "" {
		return nil
	}
	return fmt.Errorf(
		"bind address %q is non-loopback and this server has no built-in "+
			"authentication; front it with a reverse proxy and pass "+
			"WithUnauthenticatedExposure() to acknowledge, enable mTLS with "+
			"WithClientCA, or bind to 127.0.0.1, [::1], or localhost",
		o.bindAddress,
	)
}

// isLoopbackBind reports whether addr refers to a loopback address. Accepts
// IP literals (including IPv6 in brackets) and the string "localhost". The
// wildcard binds (empty host, "0.0.0.0", "::") are reported non-loopback.
func isLoopbackBind(addr string) (bool, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false, fmt.Errorf("invalid bind address %q: %w", addr, err)
	}
	if host == "" {
		return false, nil
	}
	if host == "localhost" {
		return true, nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false, fmt.Errorf("bind host %q must be an IP literal or \"localhost\"", host)
	}
	return ip.IsLoopback(), nil
}

// buildTLSConfig assembles a tls.Config when the caller provided TLS options.
// It returns nil when TLS is not configured. When a client CA is provided
// without a server cert, construction fails rather than silently disabling
// mTLS.
func buildTLSConfig(o *Options) (*tls.Config, error) {
	if o.clientCAFile != "" && o.tlsCertFile == "" {
		return nil, fmt.Errorf("WithClientCA requires WithTLS")
	}
	if o.tlsCertFile == "" {
		return nil, nil
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if o.clientCAFile != "" {
		pool := x509.NewCertPool()
		pem, err := os.ReadFile(o.clientCAFile)
		if err != nil {
			return nil, fmt.Errorf("reading client CA file: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no certificates found in client CA file %q", o.clientCAFile)
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg, nil
}

// chain applies middlewares in reverse so the first argument is the outermost
// wrapper.
func chain(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// discardLogger drops all log output. Used when WithLogger is not supplied.
type discardLogger struct{}

func (*discardLogger) Debugf(string, ...any) {}
func (*discardLogger) Infof(string, ...any)  {}
func (*discardLogger) Warnf(string, ...any)  {}
func (*discardLogger) Errorf(string, ...any) {}
