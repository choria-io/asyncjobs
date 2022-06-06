// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	aj "github.com/choria-io/asyncjobs"
	"github.com/choria-io/fisk"
	"github.com/dustin/go-humanize"
)

type taskCronCommand struct {
	name     string
	schedule string
	ttype    string
	payload  string
	queue    string
	deadline time.Duration
	maxtries int
	promPort int

	names bool
	force bool
	json  bool
}

func configureTaskCronCommand(app *fisk.CmdClause) {
	c := &taskCronCommand{}

	cron := app.Command("cron", "Tasks Scheduler Management and Scheduler").Alias("c")

	add := cron.Command("add", "Adds a Scheduled Task entry").Alias("new").Alias("a").Action(c.addAction)
	add.Arg("name", "Unique name for this Scheduled Task").Required().StringVar(&c.name)
	add.Arg("schedule", "Schedule definition").Required().StringVar(&c.schedule)
	add.Arg("type", "The task type").Required().StringVar(&c.ttype)
	add.Flag("payload", "The task Payload").StringVar(&c.payload)
	add.Flag("queue", "The name of the queue to add the task to").Short('q').Default("DEFAULT").StringVar(&c.queue)
	add.Flag("deadline", "A duration to determine when the latest time that a task handler will be called").DurationVar(&c.deadline)
	add.Flag("tries", "Sets the maximum amount of times this task may be tried").IntVar(&c.maxtries)

	view := cron.Command("view", "Views a Scheduled Task").Alias("show").Alias("v").Action(c.viewAction)
	view.Arg("name", "The name of the Schedule to view").Required().StringVar(&c.name)
	view.Flag("json", "Show JSON data").Short('j').BoolVar(&c.json)

	rm := cron.Command("delete", "Removes a Scheduled Task entry").Alias("del").Alias("rm").Action(c.rmAction)
	rm.Arg("name", "The name of the Schedule to view").Required().StringVar(&c.name)
	rm.Flag("force", "Force removal without prompting").Short('f').BoolVar(&c.force)

	ls := cron.Command("list", "List Scheduled Tasks").Alias("ls").Action(c.lsAction)
	ls.Flag("names", "Show only task names").BoolVar(&c.names)

	scheduler := cron.Command("scheduler", "Runs the Scheduled Task Scheduler").Action(c.schedulerAction)
	scheduler.Arg("name", "A unique name for this scheduler, used for leader election").Required().Envar("AJC_SCHEDULER_NAME").StringVar(&c.name)
	scheduler.Flag("monitor", "Runs monitoring on the given port").PlaceHolder("AJC_MONITOR").IntVar(&c.promPort)
}

func (c *taskCronCommand) schedulerAction(_ *fisk.ParseContext) error {
	err := prepare(aj.PrometheusListenPort(c.promPort))
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := sync.WaitGroup{}

	sched, err := aj.NewTaskScheduler(c.name, client)
	if err != nil {
		return err
	}

	err = sched.Run(ctx, &wg)
	if err != nil {
		log.Errorf("Scheduler failed: %v", err)
	}

	log.Infof("Waiting for background tasks to finish")
	wg.Wait()

	return nil
}

func (c *taskCronCommand) lsAction(_ *fisk.ParseContext) error {
	err := prepare()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tasks, err := client.ScheduledTasksStorage().ScheduledTasks(ctx)
	if err != nil {
		return err
	}

	if c.names {
		for _, t := range tasks {
			fmt.Println(t.Name)
		}
		return nil
	}

	if len(tasks) == 0 {
		fmt.Println("No Scheduled Tasks found")
		return nil
	}

	table := newTableWriter(fmt.Sprintf("%d Scheduled Task(s)", len(tasks)))
	table.AddHeaders("Name", "Schedule", "Queue", "Task Type", "Created")
	for _, v := range tasks {
		table.AddRow(v.Name, v.Schedule, v.Queue, v.TaskType, v.CreatedAt.Format(timeFormat))
	}
	fmt.Println(table.Render())

	return nil
}

func (c *taskCronCommand) rmAction(_ *fisk.ParseContext) error {
	err := prepare()
	if err != nil {
		return err
	}

	if !c.force {
		ok, err := askConfirmation(fmt.Sprintf("Really remove Scheduled Task %s", c.name), false)
		if err != nil || !ok {
			return err
		}
	}

	err = client.RemoveScheduledTask(c.name)
	if err != nil {
		return err
	}

	fmt.Printf("Removed Scheduled Task %s\n", c.name)

	return nil
}

func (c *taskCronCommand) viewAction(_ *fisk.ParseContext) error {
	err := prepare()
	if err != nil {
		return err
	}

	i, err := client.LoadScheduledTaskByName(c.name)
	if err != nil {
		return err
	}

	c.showSchedule(i)

	return nil
}

func (c *taskCronCommand) showSchedule(s *aj.ScheduledTask) {
	if c.json {
		j, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			return
		}
		fmt.Printf("%s\n", string(j))
		return
	}

	fmt.Printf("Scheduled Task %v created at %v\n\n", s.Name, s.CreatedAt.Format(timeFormat))
	fmt.Printf("             Schedule: %s\n", s.Schedule)
	fmt.Printf("                Queue: %s\n", s.Queue)
	fmt.Printf("            Task Type: %s\n", s.TaskType)
	fmt.Printf("              Payload: %s\n", humanize.IBytes(uint64(len(s.Payload))))
	if s.Deadline > 0 {
		fmt.Printf("  Scheduling Deadline: %s\n", humanizeDuration(s.Deadline))
	}
	if s.MaxTries > 0 {
		fmt.Printf("        Maximum Tries: %s\n", humanize.Comma(int64(s.MaxTries)))
	}
}

func (c *taskCronCommand) addAction(_ *fisk.ParseContext) error {
	err := prepare()
	if err != nil {
		return err
	}

	var opts []aj.TaskOpt
	if c.deadline > 0 {
		opts = append(opts, aj.TaskDeadline(time.Now().UTC().Add(c.deadline)))
	}

	if c.maxtries > 0 {
		opts = append(opts, aj.TaskMaxTries(c.maxtries))
	}

	var payload interface{}
	if c.payload != "" {
		payload = c.payload
	}

	task, err := aj.NewTask(c.ttype, payload, opts...)
	if err != nil {
		return err
	}

	for _, f := range strings.Fields("yearly annually monthly weekly daily midnight hourly every") {
		if strings.HasPrefix(c.schedule, f) {
			c.schedule = "@" + c.schedule
		}
	}

	err = client.NewScheduledTask(c.name, c.schedule, c.queue, task)
	if err != nil {
		return err
	}

	st, err := client.LoadScheduledTaskByName(c.name)
	if err != nil {
		return err
	}

	c.showSchedule(st)

	return nil
}
