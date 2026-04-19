// Copyright (c) 2026, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/choria-io/asyncjobs"
	"github.com/choria-io/asyncjobs/httpserver"
	"github.com/choria-io/fisk"
)

type serverCommand struct {
	bind               string
	tlsCert            string
	tlsKey             string
	tlsClientCA        string
	queue              string
	allowCreateDefault bool
	readTimeout        time.Duration
	writeTimeout       time.Duration
	maxBody            int64
	unsafeBind         bool
}

func configureServerCommand(app *fisk.Application) {
	c := &serverCommand{}

	server := app.Command("server", "Manage the asyncjobs HTTP API server")

	run := server.Command("run", "Run the HTTP API server").Action(c.runAction)
	run.Flag("bind", "Address to listen on").Default("127.0.0.1:8080").StringVar(&c.bind)
	run.Flag("tls-cert", "TLS certificate file (enables TLS when paired with --tls-key)").ExistingFileVar(&c.tlsCert)
	run.Flag("tls-key", "TLS key file").ExistingFileVar(&c.tlsKey)
	run.Flag("tls-client-ca", "CA bundle used to verify client certificates; enables mTLS").ExistingFileVar(&c.tlsClientCA)
	run.Flag("queue", "Work queue the server enqueues tasks to (must already exist)").StringVar(&c.queue)
	run.Flag("allow-create-default", "Allow auto-creating the implicit DEFAULT work queue").UnNegatableBoolVar(&c.allowCreateDefault)
	run.Flag("read-timeout", "Per-request read timeout").Default("30s").DurationVar(&c.readTimeout)
	run.Flag("write-timeout", "Per-request write timeout").Default("30s").DurationVar(&c.writeTimeout)
	run.Flag("max-body", "Maximum request body size in bytes").Default("524288").Int64Var(&c.maxBody)
	run.Flag("unsafe-bind", "Acknowledge that binding a non-loopback address without mTLS exposes an unauthenticated server; the deployer is responsible for fronting with a proxy").BoolVar(&c.unsafeBind)
}

func (c *serverCommand) validate() error {
	if c.queue == "" && !c.allowCreateDefault {
		return fmt.Errorf("--queue is required unless --allow-create-default is set")
	}
	if (c.tlsCert == "") != (c.tlsKey == "") {
		return fmt.Errorf("both --tls-cert and --tls-key must be supplied together")
	}
	if c.tlsClientCA != "" && c.tlsCert == "" {
		return fmt.Errorf("--tls-client-ca requires --tls-cert and --tls-key")
	}
	return nil
}

func (c *serverCommand) clientOptions() []asyncjobs.ClientOpt {
	opts := []asyncjobs.ClientOpt{}
	if c.queue != "" {
		opts = append(opts, asyncjobs.BindWorkQueue(c.queue))
	}
	return opts
}

func (c *serverCommand) serverOptions() []httpserver.Option {
	opts := []httpserver.Option{
		httpserver.WithBindAddress(c.bind),
		httpserver.WithLogger(log),
		httpserver.WithReadTimeout(c.readTimeout),
		httpserver.WithWriteTimeout(c.writeTimeout),
		httpserver.WithMaxBodyBytes(c.maxBody),
	}
	if c.unsafeBind {
		opts = append(opts, httpserver.WithUnauthenticatedExposure())
	}
	if c.allowCreateDefault {
		opts = append(opts, httpserver.WithAllowCreateDefaultQueue())
	}
	if c.tlsCert != "" {
		opts = append(opts, httpserver.WithTLS(c.tlsCert, c.tlsKey))
	}
	if c.tlsClientCA != "" {
		opts = append(opts, httpserver.WithClientCA(c.tlsClientCA))
	}
	return opts
}

func (c *serverCommand) runAction(_ *fisk.ParseContext) error {
	if err := c.validate(); err != nil {
		return err
	}

	if err := prepare(c.clientOptions()...); err != nil {
		return err
	}

	srv, err := httpserver.NewServer(client, c.serverOptions()...)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if c.unsafeBind && c.tlsClientCA == "" {
		log.Warnf("unsafe-bind: listening on %s with no built-in authentication; a reverse proxy must terminate before this listener", c.bind)
	}

	go func() {
		<-ctx.Done()
		log.Infof("Shutdown signal received, draining connections")
	}()

	return srv.Run(ctx)
}
