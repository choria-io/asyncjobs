// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"time"

	"github.com/choria-io/asyncjobs"
	"github.com/choria-io/fisk"
)

type infoCommand struct {
	replicas  uint
	memory    bool
	retention time.Duration
}

func configureInfoCommand(app *fisk.Application) {
	c := &infoCommand{}

	info := app.Command("info", "Shows general Task and Queue information").Action(c.infoAction)
	info.Flag("replica", "When initializing, do so with this many replicas").Short('R').Default("1").UintVar(&c.replicas)
	info.Flag("memory", "When initializing, do so with memory storage").UnNegatableBoolVar(&c.memory)
	info.Flag("retention", "When initializing, sets how long Tasks are kept").DurationVar(&c.retention)
}

func (c *infoCommand) infoAction(_ *fisk.ParseContext) error {
	opts := []asyncjobs.ClientOpt{asyncjobs.StoreReplicas(c.replicas), asyncjobs.TaskRetention(c.retention)}
	if c.memory {
		opts = append(opts, asyncjobs.MemoryStorage())
	}

	err := prepare(opts...)
	if err != nil {
		return err
	}

	tasks, err := admin.TasksInfo()
	if err != nil {
		return err
	}

	showTasks(tasks)
	fmt.Println()

	cfg, err := admin.ConfigurationInfo()
	if err != nil {
		return err
	}
	showConfig(cfg)
	fmt.Println()

	es, err := admin.ElectionStorage()
	if err == nil {
		showElectionStatus(es)
		fmt.Println()
	}

	queues, err := admin.Queues()
	if err != nil {
		return err
	}

	for _, q := range queues {
		showQueue(q)

		fmt.Println()
	}
	return nil
}
