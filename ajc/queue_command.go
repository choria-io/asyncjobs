// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/dustin/go-humanize"
	"gopkg.in/alecthomas/kingpin.v2"
)

type queueCommand struct {
	name  string
	force bool

	maxAge        time.Duration
	maxEntries    int
	maxTries      int
	maxTime       time.Duration
	maxConcurrent int
}

func configureQueueCommand(app *kingpin.Application) {
	c := &queueCommand{}

	queues := app.Command("queues", "Manage Work Queues").Alias("q").Alias("queue")

	queues.Command("list", "List Queues").Alias("ls").Action(c.lsAction)

	rm := queues.Command("delete", "Removes the entire work queue").Alias("rm").Action(c.rmAction)
	rm.Arg("queue", "Queue to remove").Required().StringVar(&c.name)
	rm.Flag("force", "Force purge without prompting").Short('f').BoolVar(&c.force)

	purge := queues.Command("purge", "Removes all items from a work queue").Action(c.purgeAction)
	purge.Arg("queue", "Queue to Purge").Required().StringVar(&c.name)
	purge.Flag("force", "Force purge without prompting").Short('f').BoolVar(&c.force)

	info := queues.Command("info", "Shows information about a queue").Alias("view").Alias("i").Action(c.viewAction)
	info.Arg("queue", "Queue to view").Required().StringVar(&c.name)

	cfg := queues.Command("configure", "Configures a Queue storage").Alias("config").Alias("cfg").Action(c.configureAction)
	cfg.Arg("queue", "Queue to Configure").Required().StringVar(&c.name)
	cfg.Flag("age", "Sets the maximum age for entries to keep, 0s for unlimited").Default("-1s").DurationVar(&c.maxAge)
	cfg.Flag("entries", "Sets the maximum amount of entries to keep, 0 for unlimited").Default("-1").IntVar(&c.maxEntries)
	cfg.Flag("tries", "Maximum delivery attempts to allow per message, -1 for unlimited").Default("-2").IntVar(&c.maxTries)
	cfg.Flag("run-time", "Maximum run-time to allow per task").Default("-1s").DurationVar(&c.maxTime)
	cfg.Flag("concurrent", "Maximum concurrent jobs that can be ran").Default("-2").IntVar(&c.maxConcurrent)
}

func (c *queueCommand) configureAction(_ *kingpin.ParseContext) error {
	err := prepare()
	if err != nil {
		return err
	}

	nfo, err := admin.QueueInfo(c.name)
	if err != nil {
		return err
	}

	scfg := nfo.Stream.Config
	ccfg := nfo.Consumer.Config

	if c.maxAge != 0 && c.maxAge > -1*time.Second && c.maxAge < 2*time.Minute {
		return fmt.Errorf("shortest max age is 2 minutes")
	}
	if c.maxAge > -1*time.Second {
		scfg.MaxAge = c.maxAge
	}

	if c.maxEntries > -1 {
		scfg.MaxMsgs = int64(c.maxEntries)
	}
	if c.maxTries > -2 {
		ccfg.MaxDeliver = c.maxTries
	}
	if c.maxTime > -1*time.Second && c.maxTime < time.Second {
		return fmt.Errorf("shortest run-time is 1 second")
	}
	if c.maxTime > -1*time.Second {
		ccfg.AckWait = c.maxTime
	}
	if c.maxConcurrent > -2 && c.maxConcurrent > 10000 {
		return fmt.Errorf("largest concurrency is 10000")
	}
	if c.maxConcurrent > -2 {
		ccfg.MaxAckPending = c.maxConcurrent
	}

	mgr, _, err := admin.TasksStore()
	if err != nil {
		return err
	}

	stream, err := mgr.LoadStream(nfo.Stream.Config.Name)
	if err != nil {
		return err
	}
	err = stream.UpdateConfiguration(scfg)
	if err != nil {
		return err
	}

	_, err = stream.NewConsumerFromDefault(ccfg)
	if err != nil {
		return err
	}

	nfo, err = admin.QueueInfo(c.name)
	if err != nil {
		return err
	}
	showQueue(nfo)

	return nil
}

func (c *queueCommand) viewAction(_ *kingpin.ParseContext) error {
	err := prepare()
	if err != nil {
		return err
	}

	nfo, err := admin.QueueInfo(c.name)
	if err != nil {
		return err
	}
	showQueue(nfo)

	return nil
}

func (c *queueCommand) purgeAction(_ *kingpin.ParseContext) error {
	err := prepare()
	if err != nil {
		return err
	}

	if !c.force {
		nfo, err := admin.QueueInfo(c.name)
		if err != nil {
			return err
		}

		ok, err := askConfirmation(fmt.Sprintf("Really purge all entries from the %s Queue with %s entries and %s active polls", c.name, humanize.Comma(int64(nfo.Stream.State.Msgs)), humanize.Comma(int64(nfo.Consumer.NumWaiting))), false)
		if err != nil || !ok {
			return err
		}
	}

	err = admin.PurgeQueue(c.name)
	if err != nil {
		return err
	}

	fmt.Printf("Queue %s was purged\n", c.name)

	return nil
}

func (c *queueCommand) rmAction(_ *kingpin.ParseContext) error {
	err := prepare()
	if err != nil {
		return err
	}

	if !c.force {
		nfo, err := admin.QueueInfo(c.name)
		if err != nil {
			return err
		}

		ok, err := askConfirmation(fmt.Sprintf("Really delete the %s Queue with %s entries and %s active polls", c.name, humanize.Comma(int64(nfo.Stream.State.Msgs)), humanize.Comma(int64(nfo.Consumer.NumWaiting))), false)
		if err != nil || !ok {
			return err
		}
	}

	err = admin.DeleteQueue(c.name)
	if err != nil {
		return err
	}

	fmt.Printf("Queue %s was removed\n", c.name)
	return nil
}

func (c *queueCommand) lsAction(_ *kingpin.ParseContext) error {
	err := prepare()
	if err != nil {
		return err
	}

	queues, err := admin.Queues()
	if err != nil {
		return err
	}

	if len(queues) == 0 {
		fmt.Printf("No queues defined\n")
		return nil
	}

	table := newTableWriter("Work Queues")
	table.AddHeaders("Name", "Items", "Size", "Replicas", "Max Tries", "Max Items", "Max Concurrent")

	sort.Slice(queues, func(i, j int) bool {
		return queues[i].Stream.State.Msgs < queues[j].Stream.State.Msgs
	})

	for _, q := range queues {
		maxMsgs := "unlimited"
		if q.Stream.Config.MaxMsgs > 0 {
			maxMsgs = humanize.Comma(q.Stream.Config.MaxMsgs)
		}

		table.AddRow(q.Name, humanize.Comma(int64(q.Stream.State.Msgs)), humanize.IBytes(q.Stream.State.Bytes), q.Stream.Config.Replicas, humanize.Comma(int64(q.Consumer.Config.MaxDeliver)), maxMsgs, humanize.Comma(int64(q.Consumer.Config.MaxAckPending)))
	}

	fmt.Println(table.Render())

	return nil
}