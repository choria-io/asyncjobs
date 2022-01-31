package main

import (
	"context"
	"fmt"
	"time"

	"github.com/choria-io/asyncjobs"
	"github.com/dustin/go-humanize"
	"github.com/nats-io/jsm.go"
	"github.com/xlab/tablewriter"
	"gopkg.in/alecthomas/kingpin.v2"
)

type taskCommand struct {
	id        string
	ttype     string
	payload   string
	queue     string
	deadline  time.Duration
	retention time.Duration

	limit int
	json  bool
	force bool
}

func configureTaskCommand(app *kingpin.Application) {
	c := taskCommand{}

	tasks := app.Command("task", "Manage Tasks").Alias("t")

	add := tasks.Command("add", "Adds a new Task to a queue").Alias("new").Alias("a").Alias("enqueue").Action(c.addAction)
	add.Arg("type", "The task type").Required().StringVar(&c.ttype)
	add.Arg("payload", "The task Payload").Required().StringVar(&c.payload)
	add.Flag("queue", "The name of the queue to add the task to").Short('q').Default("DEFAULT").StringVar(&c.queue)
	add.Flag("deadline", "A duration to determine when the latest time that a task handler will be called").DurationVar(&c.deadline)

	view := tasks.Command("view", "Views the status of a Task").Alias("show").Alias("v").Action(c.viewAction)
	view.Arg("id", "The Task ID to view").Required().StringVar(&c.id)
	view.Flag("json", "Show JSON data").Short('j').BoolVar(&c.json)

	rm := tasks.Command("delete", "Removes a task from the tasks storage").Alias("del").Alias("rm").Action(c.rmAction)
	rm.Arg("id", "The Task ID to remove").Required().StringVar(&c.id)
	rm.Flag("force", "Force removal without prompting").Short('f').BoolVar(&c.force)

	ls := tasks.Command("list", "List Tasks").Alias("ls").Action(c.lsAction)
	ls.Arg("limit", "Limits the number of tasks shown").Default("200").IntVar(&c.limit)

	purge := tasks.Command("purge", "Purge all entries from the Tasks store").Action(c.purgeAction)
	purge.Flag("force", "Force purge without prompting").Short('f').BoolVar(&c.force)

	config := tasks.Command("configure", "Configures the Task storage").Alias("cfg").Action(c.configAction)
	config.Arg("retention", "Sets how long Tasks are kept in the Task Store").Required().DurationVar(&c.retention)
}

func (c *taskCommand) purgeAction(_ *kingpin.ParseContext) error {
	err := prepare()
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

		ok, err := askConfirmation(fmt.Sprintf("Really purge the Task Store of %d entries, work queue entries will not be removed", nfo.Msgs), false)
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

func (c *taskCommand) configAction(_ *kingpin.ParseContext) error {
	err := prepare()
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

	err = tasks.UpdateConfiguration(*cfg, jsm.MaxAge(c.retention))
	if err != nil {
		return err
	}

	return infoAction(nil)
}

func (c *taskCommand) lsAction(_ *kingpin.ParseContext) error {
	err := prepare()
	if err != nil {
		return err
	}

	nfo, err := admin.TasksInfo()
	if err != nil {
		return err
	}

	tasks, err := admin.Tasks(context.Background(), int32(c.limit))
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

	for task := range tasks {
		table.AddRow(task.ID, task.Type, task.CreatedAt.Format(time.RFC822), task.State, task.Queue, task.Tries)
	}

	fmt.Println(table.Render())
	return nil
}

func (c *taskCommand) rmAction(_ *kingpin.ParseContext) error {
	err := prepare()
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

func (c *taskCommand) viewAction(_ *kingpin.ParseContext) error {
	err := prepare()
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

	fmt.Printf("Task %s created at %s\n\n", task.ID, task.CreatedAt.Format(time.RFC822))
	fmt.Printf("              Payload: %s\n", humanize.IBytes(uint64(len(task.Payload))))
	fmt.Printf("               Status: %s\n", task.State)
	if task.Queue != "" {
		fmt.Printf("                Queue: %s\n", task.Queue)
	}
	fmt.Printf("                Tries: %d\n", task.Tries)
	if task.LastTriedAt != nil {
		fmt.Printf("       Last Processed: %s\n", task.LastTriedAt.Format(time.RFC822))
	}
	if task.LastErr != "" {
		fmt.Printf("           Last Error: %s\n", task.LastErr)
	}
	if task.Deadline != nil {
		fmt.Printf("  Scheduling Deadline: %s\n", task.Deadline.Format(time.RFC822))
	}

	return nil
}

func (c *taskCommand) addAction(_ *kingpin.ParseContext) error {
	err := prepare()
	if err != nil {
		return err
	}

	var opts []asyncjobs.TaskOpt
	if c.deadline > 0 {
		opts = append(opts, asyncjobs.TaskDeadline(time.Now().UTC().Add(c.deadline)))
	}

	task, err := asyncjobs.NewTask(c.ttype, c.payload, opts...)
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
