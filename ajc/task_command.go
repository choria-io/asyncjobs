// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	aj "github.com/choria-io/asyncjobs"
	"github.com/choria-io/fisk"
	"github.com/dustin/go-humanize"
	"github.com/nats-io/jsm.go"
	"github.com/xlab/tablewriter"
)

type taskCommand struct {
	id              string
	ttype           string
	payload         string
	queue           string
	deadline        time.Duration
	maxtries        int
	retention       time.Duration
	concurrency     int
	command         string
	promPort        int
	memory          bool
	replicas        int
	retry           string
	remote          bool
	discardComplete bool
	discardExpired  bool
	dependencies    []string
	loadDepResults  bool
	ed25519Seed     string
	ed25519PubKey   string
	optionalSigs    bool
	maxBytes        int64
	maxBytesSet     bool

	limit int
	json  bool
	force bool
}

func configureTaskCommand(app *fisk.Application) {
	c := &taskCommand{}

	tasks := app.Command("tasks", "Manage Tasks").Alias("t").Alias("task")
	tasks.Flag("sign", "Signs tasks using an ed25519 seed").StringVar(&c.ed25519Seed)
	tasks.Flag("verify", "Verifies tasks using an ed25519 public key").StringVar(&c.ed25519PubKey)

	add := tasks.Command("add", "Adds a new Task to a queue").Alias("new").Alias("a").Alias("enqueue").Action(c.addAction)
	add.Arg("type", "The task type").Required().StringVar(&c.ttype)
	add.Arg("payload", "The task Payload").Required().StringVar(&c.payload)
	add.Flag("queue", "The name of the queue to add the task to").Short('q').Default("DEFAULT").StringVar(&c.queue)
	add.Flag("deadline", "A duration to determine when the latest time that a task handler will be called").DurationVar(&c.deadline)
	add.Flag("tries", "Sets the maximum amount of times this task may be tried").IntVar(&c.maxtries)
	add.Flag("depends", "Sets IDs to depend on, comma sep or pass multiple times").StringsVar(&c.dependencies)
	add.Flag("load", "Loads results from dependencies before executing task").BoolVar(&c.loadDepResults)

	retry := tasks.Command("retry", "Retries delivery of a task currently in the Task Store").Action(c.retryAction)
	retry.Arg("id", "The Task ID to view").Required().StringVar(&c.id)
	retry.Flag("queue", "The name of the queue to add the task to").Short('q').Default("DEFAULT").StringVar(&c.queue)

	view := tasks.Command("view", "Views the status of a Task").Alias("show").Alias("v").Alias("info").Alias("i").Action(c.viewAction)
	view.Arg("id", "The Task ID to view").Required().StringVar(&c.id)
	view.Flag("json", "Show JSON data").Short('j').BoolVar(&c.json)

	rm := tasks.Command("delete", "Removes a task from the tasks storage").Alias("del").Alias("rm").Action(c.rmAction)
	rm.Arg("id", "The Task ID to remove").Required().StringVar(&c.id)
	rm.Flag("force", "Force removal without prompting").Short('f').BoolVar(&c.force)

	ls := tasks.Command("list", "List Tasks").Alias("ls").Action(c.lsAction)
	ls.Arg("limit", "Limits the number of tasks shown").Default("200").IntVar(&c.limit)

	purge := tasks.Command("purge", "Purge all entries from the Tasks store").Action(c.purgeAction)
	purge.Flag("force", "Force purge without prompting").Short('f').BoolVar(&c.force)

	init := tasks.Command("initialize", "Initialize the Task storage").Action(c.initAction)
	init.Flag("memory", "Use memory as a storage backend").BoolVar(&c.memory)
	init.Flag("retention", "Sets how long Tasks are kept in the Task Store").DurationVar(&c.retention)
	init.Flag("replicas", "How many replicas to keep in a JetStream cluster").Default("1").IntVar(&c.replicas)
	init.Flag("max-bytes", "Maximum bytes that can be stored in the queue, -1 for unlimited").Default("-1").IsSetByUser(&c.maxBytesSet).Int64Var(&c.maxBytes)

	config := tasks.Command("configure", "Configures the Task storage").Alias("config").Alias("cfg").Action(c.configAction)
	config.Arg("retention", "Sets how long Tasks are kept in the Task Store").Required().DurationVar(&c.retention)
	config.Arg("replicas", "How many replicas to keep in a JetStream cluster").Required().IntVar(&c.replicas)

	watch := tasks.Command("watch", "Watch job updates in real time").Action(c.watchAction)
	watch.Flag("task", "Watch for updates related to a specific task ID").StringVar(&c.id)

	policies := aj.RetryPolicyNames()
	process := tasks.Command("process", "Process Tasks from a given queue").Action(c.processAction)
	process.Arg("type", "Types of Tasks to process").Required().Envar("AJC_TYPE").StringVar(&c.ttype)
	process.Arg("queue", "The Queue to consume Tasks from").Required().Envar("AJC_QUEUE").StringVar(&c.queue)
	process.Arg("concurrency", "How many concurrent Tasks to process").Required().Envar("AJC_CONCURRENCY").IntVar(&c.concurrency)
	process.Arg("command", "The command to invoke for each Task").Envar("AJC_COMMAND").ExistingFileVar(&c.command)
	process.Flag("remote", "Process tasks using a remote request-reply callout").BoolVar(&c.remote)
	process.Flag("monitor", "Runs monitoring on the given port").PlaceHolder("AJC_MONITOR").IntVar(&c.promPort)
	process.Flag("discard-complete", "Discard messages in the 'complete' state").BoolVar(&c.discardComplete)
	process.Flag("discard-expired", "Discard messages in the 'expired' state").BoolVar(&c.discardExpired)
	process.Flag("backoff", fmt.Sprintf("Selects a backoff policy to apply (%s)", strings.Join(policies, ", "))).Default("default").EnumVar(&c.retry, policies...)

	configureTaskCronCommand(tasks)
}

func (c *taskCommand) prepare(copts ...aj.ClientOpt) error {
	sigOpts, err := c.clientOpts()
	if err != nil {
		return err
	}

	return prepare(append(copts, sigOpts...)...)
}

func (c *taskCommand) clientOpts() ([]aj.ClientOpt, error) {
	var opts []aj.ClientOpt

	if c.optionalSigs {
		opts = append(opts, aj.TaskSignaturesOptional())
	}

	if c.ed25519Seed != "" {
		if fileExist(c.ed25519Seed) {
			opts = append(opts, aj.TaskSigningSeedFile(c.ed25519Seed))
		} else {
			seed, err := hex.DecodeString(c.ed25519Seed)
			if err != nil {
				return nil, err
			}

			opts = append(opts, aj.TaskSigningKey(ed25519.NewKeyFromSeed(seed)))
		}
	}

	if c.ed25519PubKey != "" {
		if fileExist(c.ed25519PubKey) {
			opts = append(opts, aj.TaskVerificationKeyFile(c.ed25519PubKey))
		} else {
			pk, err := hex.DecodeString(c.ed25519PubKey)
			if err != nil {
				return nil, err
			}
			opts = append(opts, aj.TaskVerificationKey(pk))
		}
	}

	return opts, nil
}

func (c *taskCommand) retryAction(_ *fisk.ParseContext) error {
	err := c.prepare(aj.BindWorkQueue(c.queue))
	if err != nil {
		return err
	}

	err = client.RetryTaskByID(context.Background(), c.id)
	if err != nil {
		return err
	}

	return c.viewAction(nil)
}

func (c *taskCommand) initAction(_ *fisk.ParseContext) error {
	err := c.prepare(aj.NoStorageInit())
	if err != nil {
		return err
	}

	err = admin.PrepareTasks(c.memory, c.replicas, c.retention, c.maxBytes, c.maxBytesSet)
	if err != nil {
		return err
	}

	nfo, err := admin.TasksInfo()
	if err != nil {
		return err
	}

	showTasks(nfo)

	return nil
}

func (c *taskCommand) watchAction(_ *fisk.ParseContext) error {
	err := c.prepare()
	if err != nil {
		return err
	}

	mgr, _, err := admin.TasksStore()
	if err != nil {
		return err
	}

	target := aj.EventsSubjectWildcard
	if c.id != "" {
		target = fmt.Sprintf(aj.TaskStateChangeEventSubjectPattern, c.id)
	}

	sub, err := mgr.NatsConn().SubscribeSync(target)
	if err != nil {
		return err
	}

	for {
		msg, err := sub.NextMsg(time.Hour)
		if err != nil {
			return err
		}

		event, kind, err := aj.ParseEventJSON(msg.Data)
		if err != nil {
			fmt.Printf("Could not parse event: %v\n", err)
		}

		switch e := event.(type) {
		case aj.TaskStateChangeEvent:
			if e.LastErr == "" {
				fmt.Printf("[%s] %s: queue: %s type: %s tries: %d state: %s\n", e.TimeStamp.Format("15:04:05"), e.TaskID, e.Queue, e.TaskType, e.Tries, e.State)
			} else {
				fmt.Printf("[%s] %s: queue: %s type: %s tries: %d state: %s error: %s\n", e.TimeStamp.Format("15:04:05"), e.TaskID, e.Queue, e.TaskType, e.Tries, e.State, e.LastErr)
			}

		case aj.LeaderElectedEvent:
			fmt.Printf("[%s] %s: new %s leader\n", e.TimeStamp.Format("15:04:05"), e.Name, e.Component)

		default:
			fmt.Printf("[%s] Unknown event type %s\n", time.Now().UTC().Format("15:04:05"), kind)
		}
	}
}

func (c *taskCommand) processAction(_ *fisk.ParseContext) error {
	if c.command == "" && !c.remote {
		return fmt.Errorf("either a command or --remote is required")
	}

	copts := []aj.ClientOpt{
		aj.BindWorkQueue(c.queue),
		aj.PrometheusListenPort(c.promPort),
		aj.RetryBackoffPolicyName(c.retry),
		aj.ClientConcurrency(c.concurrency)}

	if c.discardComplete {
		copts = append(copts, aj.DiscardTaskStates(aj.TaskStateCompleted))
	}
	if c.discardExpired {
		copts = append(copts, aj.DiscardTaskStates(aj.TaskStateExpired))
	}

	err := c.prepare(copts...)
	if err != nil {
		return err
	}

	router := aj.NewTaskRouter()
	if c.remote {
		err = router.RequestReply(c.ttype, client)
	} else {
		err = router.ExternalProcess(c.ttype, c.command)
	}
	if err != nil {
		return err
	}

	return client.Run(context.Background(), router)
}

func (c *taskCommand) purgeAction(_ *fisk.ParseContext) error {
	err := c.prepare()
	if err != nil {
		return err
	}

	_, stream, err := admin.TasksStore()
	if err != nil {
		return err
	}

	if !c.force {
		nfo, err := stream.State()
		if err != nil {
			return err
		}

		ok, err := askConfirmation(fmt.Sprintf("Really purge the Task Store of %s entries, work queue entries will not be removed", humanize.Comma(int64(nfo.Msgs))), false)
		if err != nil || !ok {
			return err
		}
	}

	err = stream.Purge()
	if err != nil {
		return err
	}

	fmt.Printf("Purged all task entries\n")

	return nil
}

func (c *taskCommand) configAction(_ *fisk.ParseContext) error {
	err := c.prepare()
	if err != nil {
		return err
	}

	_, tasks, err := admin.TasksStore()
	if err != nil {
		return err
	}

	cfg, err := jsm.NewStreamConfiguration(tasks.Configuration())
	if err != nil {
		return err
	}

	err = tasks.UpdateConfiguration(*cfg, jsm.MaxAge(c.retention), jsm.Replicas(c.replicas))
	if err != nil {
		return err
	}

	store, err := admin.TasksInfo()
	if err != nil {
		return err
	}

	showTasks(store)

	return nil
}

func (c *taskCommand) lsAction(_ *fisk.ParseContext) error {
	err := c.prepare()
	if err != nil {
		return err
	}

	nfo, err := admin.TasksInfo()
	if err != nil {
		return err
	}

	var table *tablewriter.Table
	if nfo.Stream.State.Msgs > uint64(c.limit) {
		table = newTableWriter(fmt.Sprintf("%d of %d Tasks", c.limit, nfo.Stream.State.Msgs))
	} else {
		table = newTableWriter(fmt.Sprintf("%d Tasks", nfo.Stream.State.Msgs))
	}
	table.AddHeaders("ID", "Type", "Created", "State", "Queue", "Tries")

	tasks, err := admin.Tasks(context.Background(), int32(c.limit))
	if err != nil {
		return err
	}

	for task := range tasks {
		table.AddRow(task.ID, task.Type, task.CreatedAt.Format(timeFormat), task.State, task.Queue, task.Tries)
	}

	fmt.Println(table.Render())
	return nil
}

func (c *taskCommand) rmAction(_ *fisk.ParseContext) error {
	err := c.prepare()
	if err != nil {
		return err
	}

	if !c.force {
		ok, err := askConfirmation(fmt.Sprintf("Really remove Task %s, work queue entries will not be removed", c.id), false)
		if err != nil || !ok {
			return err
		}
	}

	err = admin.DeleteTaskByID(c.id)
	if err != nil {
		return err
	}

	fmt.Printf("Removed Task %s\n", c.id)

	return nil
}

func (c *taskCommand) viewAction(_ *fisk.ParseContext) error {
	err := c.prepare()
	if err != nil {
		return err
	}

	task, err := client.LoadTaskByID(c.id)
	if err != nil {
		return err
	}

	if c.json {
		dumpJSON(task)
		return nil
	}

	fmt.Printf("Task %s created at %s\n\n", task.ID, task.CreatedAt.Format(timeFormat))
	fmt.Printf("            Task Type: %s\n", task.Type)
	fmt.Printf("              Payload: %s\n", humanize.IBytes(uint64(len(task.Payload))))
	fmt.Printf("               Status: %s\n", task.State)
	if task.HasDependencies() {
		fmt.Printf("         Dependencies: %v\n", strings.Join(task.Dependencies, ", "))
		fmt.Printf("     Load Dep Results: %t\n", task.LoadDependencies)
	}
	if task.Result != nil {
		fmt.Printf("            Completed: %s (%s)\n", task.Result.CompletedAt.Format(timeFormat), humanizeDuration(task.Result.CompletedAt.Sub(task.CreatedAt)))
	} else {
		if task.LastTriedAt != nil {
			fmt.Printf("       Last Processed: %s\n", task.LastTriedAt.Format(timeFormat))
		}
		if task.LastErr != "" {
			fmt.Printf("           Last Error: %s\n", task.LastErr)
		}
	}
	if task.Queue != "" {
		fmt.Printf("                Queue: %s\n", task.Queue)
	}
	fmt.Printf("                Tries: %d\n", task.Tries)
	if task.Deadline != nil {
		fmt.Printf("  Scheduling Deadline: %s\n", task.Deadline.Format(timeFormat))
	}
	if task.MaxTries > 0 {
		fmt.Printf("        Maximum Tries: %s\n", humanize.Comma(int64(task.MaxTries)))
	}

	return nil
}

func (c *taskCommand) addAction(_ *fisk.ParseContext) error {
	err := c.prepare(aj.BindWorkQueue(c.queue))
	if err != nil {
		return err
	}

	var opts []aj.TaskOpt
	if c.deadline > 0 {
		opts = append(opts, aj.TaskDeadline(time.Now().UTC().Add(c.deadline)))
	}

	if len(c.dependencies) > 0 {
		for _, deps := range c.dependencies {
			for _, dep := range strings.Split(deps, ",") {
				opts = append(opts, aj.TaskDependsOnIDs(strings.TrimSpace(dep)))
			}
		}

		if c.loadDepResults {
			opts = append(opts, aj.TaskRequiresDependencyResults())
		}
	}

	if c.maxtries > 0 {
		opts = append(opts, aj.TaskMaxTries(c.maxtries))
	}

	task, err := aj.NewTask(c.ttype, c.payload, opts...)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = client.EnqueueTask(ctx, task)
	if err != nil {
		return err
	}

	fmt.Printf("Enqueued task %s\n", task.ID)

	return nil
}
