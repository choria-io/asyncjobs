// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

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

	showTasks(tasks)

	fmt.Println()

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
