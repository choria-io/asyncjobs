package main

import (
	"fmt"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/nats-io/jsm.go/api"
	"gopkg.in/alecthomas/kingpin.v2"
)

func configureInfoCommand(app *kingpin.Application) {
	app.Command("info", "Shows general Task and Queue information").Action(infoAction)
}

func infoAction(_ *kingpin.ParseContext) error {
	err := prepare()
	if err != nil {
		return err
	}

	tasks, err := admin.TasksInfo()
	if err != nil {
		return err
	}

	fmt.Printf("Tasks Storage:\n\n")
	nfo := tasks.Stream
	fmt.Printf("         Entries: %d\n", nfo.State.Msgs)
	fmt.Printf("            Size: %s\n", humanize.IBytes(nfo.State.Bytes))
	fmt.Printf("         Storage: %v\n", nfo.Config.Storage.String())
	fmt.Printf("        Replicas: %d\n", nfo.Config.Replicas)
	fmt.Printf("    Memory Based: %t\n", nfo.Config.Storage == api.MemoryStorage)
	fmt.Printf("  Archive Period: %s\n", humanizeDuration(nfo.Config.MaxAge))
	if !nfo.State.FirstTime.IsZero() {
		fmt.Printf("      First Task: %v (%s)\n", nfo.State.FirstTime, humanizeDuration(time.Since(nfo.State.FirstTime)))
	}
	if !nfo.State.LastTime.IsZero() {
		fmt.Printf("       Last Task: %v (%s)\n", nfo.State.LastTime, humanizeDuration(time.Since(nfo.State.LastTime)))
	}

	fmt.Println()

	queues, err := admin.Queues()
	if err != nil {
		return err
	}

	for _, q := range queues {
		fmt.Printf("%s Work Queue:\n\n", q.Name)
		fmt.Printf("         Entries: %s\n", humanize.Comma(int64(q.Stream.State.Msgs)))
		fmt.Printf("            Size: %s\n", humanize.IBytes(q.Stream.State.Bytes))
		fmt.Printf("        Priority: %s\n", q.Consumer.Config.Description)
		fmt.Printf("         Storage: %v\n", q.Stream.Config.Storage.String())
		fmt.Printf("        Replicas: %d\n", q.Stream.Config.Replicas)
		fmt.Printf("  Archive Period: %s\n", humanizeDuration(q.Stream.Config.MaxAge))
		if !q.Stream.State.FirstTime.IsZero() {
			fmt.Printf("      First Task: %v (%s)\n", q.Stream.State.FirstTime, humanizeDuration(time.Since(q.Stream.State.FirstTime)))
		}
		if !q.Stream.State.LastTime.IsZero() {
			fmt.Printf("       Last Task: %v (%s)\n", q.Stream.State.LastTime, humanizeDuration(time.Since(q.Stream.State.LastTime)))
		}
		fmt.Printf("  Max Task Tries: %d\n", q.Consumer.Config.MaxDeliver)
		fmt.Printf("    Max Run Time: %s\n", humanizeDuration(q.Consumer.Config.AckWait))
		fmt.Printf("  Max Concurrent: %d\n", q.Consumer.Config.MaxAckPending)

		if q.Stream.Config.MaxMsgs == -1 {
			fmt.Printf("     Max Entries: unlimited\n")
		} else {
			fmt.Printf("     Max Entries: %s\n", humanize.Comma(q.Stream.Config.MaxMsgs))
		}
		fmt.Printf("     Discard Old: %t\n", q.Stream.Config.Discard == api.DiscardOld)
		fmt.Println()
	}
	return nil
}
