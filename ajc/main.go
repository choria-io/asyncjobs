// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"

	"github.com/choria-io/asyncjobs"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	Version = "development"

	nctx  string
	debug bool
	log   *logrus.Entry

	client *asyncjobs.Client
	admin  asyncjobs.StorageAdmin

	caj *kingpin.Application
)

func main() {
	caj = kingpin.New("ajc", "Choria Asynchronous Jobs")
	caj.Version(Version)
	caj.Author("R.I.Pienaar <rip@devco.net>")

	caj.Flag("context", "NATS Context to use for connecting to JetStream").PlaceHolder("NAME").Envar("CONTEXT").Default("AJC").StringVar(&nctx)
	caj.Flag("debug", "Enable debug logging").BoolVar(&debug)

	configureInfoCommand(caj)
	configureTaskCommand(caj)

	kingpin.MustParse(caj.Parse(os.Args[1:]))
}
