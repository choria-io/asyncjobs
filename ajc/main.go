// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/choria-io/asyncjobs"
	"github.com/choria-io/fisk"
	"github.com/nats-io/jsm.go/natscontext"
	"github.com/sirupsen/logrus"
)

var (
	version    = "development"
	timeFormat = "02 Jan 06 15:04:05 MST"

	nctx   string
	debug  bool
	log    *logrus.Entry
	client *asyncjobs.Client
	admin  asyncjobs.StorageAdmin
	ajc    *fisk.Application
)

func main() {
	ajc = fisk.New("ajc", "Choria Asynchronous Jobs")
	ajc.Version(version)
	ajc.Author("R.I.Pienaar <rip@devco.net>")
	ajc.UsageWriter(os.Stdout)
	ajc.UsageTemplate(fisk.CompactMainUsageTemplate)
	ajc.ErrorUsageTemplate(fisk.CompactMainUsageTemplate)
	ajc.HelpFlag.Short('h')

	ajc.Flag("context", "NATS Context to use for connecting to JetStream").PlaceHolder("NAME").Envar("CONTEXT").Default("AJC").StringVar(&nctx)
	ajc.Flag("debug", "Enable debug level logging").Envar("AJC_DEBUG").BoolVar(&debug)

	configureInfoCommand(ajc)
	configureTaskCommand(ajc)
	configureQueueCommand(ajc)
	configurePackagesCommand(ajc)

	_, err := ajc.Parse(os.Args[1:])
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "unknown context"):
			fmt.Fprintf(os.Stderr, "ajc: no NATS context %q found, create one using 'nats context'\n", nctx)

			known := natscontext.KnownContexts()
			if len(known) > 0 {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintf(os.Stderr, "Known contexts: %s\n", strings.Join(known, "\n                "))
			}

		default:
			fmt.Fprintf(os.Stderr, "ajc runtime error: %v\n", err)
			fmt.Fprintln(os.Stderr)
			ajc.Usage(os.Args[1:])
		}

		os.Exit(1)
	}
}
