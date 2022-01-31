// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/choria-io/asyncjobs"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"github.com/xlab/tablewriter"
	"golang.org/x/term"
)

func prepare(copts ...asyncjobs.ClientOpt) error {
	logger := logrus.New()
	if debug {
		logger.SetLevel(logrus.DebugLevel)
	} else {
		logger.SetLevel(logrus.InfoLevel)
	}
	log = logrus.NewEntry(logger)

	if nctx == "" {
		return fmt.Errorf("no NATS Context specified")
	}

	var err error

	conn := []nats.Option{
		nats.MaxReconnects(10),
		nats.Name("Choria Asynchronous Jobs CLI Version " + Version),
		nats.ErrorHandler(func(nc *nats.Conn, _ *nats.Subscription, err error) {
			url := nc.ConnectedUrl()
			if url == "" {
				log.Printf("Unexpected NATS error: %s", err)
			} else {
				log.Printf("Unexpected NATS error from server %s: %s", url, err)
			}
		}),
	}

	opts := []asyncjobs.ClientOpt{
		asyncjobs.CustomLogger(log), asyncjobs.NatsContext(nctx, conn...),
	}
	opts = append(opts, copts...)

	client, err = asyncjobs.NewClient(opts...)
	if err != nil {
		return err
	}

	admin = client.StorageAdmin()

	return nil
}

func humanizeDuration(d time.Duration) string {
	if d == math.MaxInt64 {
		return "never"
	}

	if d == 0 {
		return "forever"
	}

	tsecs := d / time.Second
	tmins := tsecs / 60
	thrs := tmins / 60
	tdays := thrs / 24
	tyrs := tdays / 365

	if tyrs > 0 {
		return fmt.Sprintf("%dy%dd%dh%dm%ds", tyrs, tdays%365, thrs%24, tmins%60, tsecs%60)
	}

	if tdays > 0 {
		return fmt.Sprintf("%dd%dh%dm%ds", tdays, thrs%24, tmins%60, tsecs%60)
	}

	if thrs > 0 {
		return fmt.Sprintf("%dh%dm%ds", thrs, tmins%60, tsecs%60)
	}

	if tmins > 0 {
		return fmt.Sprintf("%dm%ds", tmins, tsecs%60)
	}

	return fmt.Sprintf("%.2fs", d.Seconds())
}

func dumpJSON(d interface{}) {
	j, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		panic(fmt.Sprintf("could not JSON render: %v", err))
	}
	fmt.Println(string(j))
}

func isTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func askConfirmation(prompt string, dflt bool) (bool, error) {
	if !isTerminal() {
		return false, fmt.Errorf("cannot ask for confirmation without a terminal")
	}

	ans := dflt

	err := survey.AskOne(&survey.Confirm{
		Message: prompt,
		Default: dflt,
	}, &ans)

	return ans, err
}

func newTableWriter(title string) *tablewriter.Table {
	table := tablewriter.CreateTable()
	table.UTF8Box()
	if title != "" {
		table.AddTitle(title)
	}

	return table
}
