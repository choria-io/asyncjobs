// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/choria-io/asyncjobs/generators"
	"gopkg.in/alecthomas/kingpin.v2"
	"gopkg.in/yaml.v2"
)

type packageCommand struct {
	file string
}

func configurePackagesCommand(app *kingpin.Application) {
	c := &packageCommand{}

	pkg := app.Command("package", "Creates packages hosting handlers").Alias("pkg")

	pkg.Command("docker", "Creates a Docker Container hosting handlers based on handlers.yaml").Action(c.dockerAction)
	pkg.Flag("file", "Use a specific configuration file rather than asyncjobs.yaml").Default("asyncjobs.yaml").ExistingFileVar(&c.file)
}

func (c *packageCommand) dockerAction(_ *kingpin.ParseContext) error {
	createLogger()

	_, err := os.Stat(c.file)
	if os.IsNotExist(err) {
		return fmt.Errorf("handlers.yaml does not exist")
	}

	hb, err := os.ReadFile(c.file)
	if err != nil {
		return err
	}

	h := &generators.Package{}
	err = yaml.Unmarshal(hb, h)
	if err != nil {
		return fmt.Errorf("invalid handlers file: %v", err)
	}

	if h.ContextName == "" {
		h.ContextName = "AJ"
	}
	if h.WorkQueue == "" {
		h.WorkQueue = "DEFAULT"
	}
	if h.AJVersion == "" {
		h.AJVersion = fmt.Sprintf("v%s", version)
	}
	if h.Name == "" {
		h.Name = "choria.io/asyncjobs/handlers"
	}
	if h.RetryPolicy == "" {
		h.RetryPolicy = "default"
	}

	if len(h.TaskHandlers) == 0 {
		return fmt.Errorf("no task handlers specified in %s", c.file)
	}

	table := newTableWriter("Handler Microservice Settings")
	table.AddRow("Package Name", h.Name)
	table.AddRow("NATS Context Name", h.ContextName)
	table.AddRow("Work Queue", h.WorkQueue)
	table.AddRow("Task Handlers", len(h.TaskHandlers))
	table.AddRow("Retry Backoff Policy", h.RetryPolicy)
	if len(h.DiscardStates) > 0 {
		table.AddRow("End State Discard", strings.Join(h.DiscardStates, ", "))
	} else {
		table.AddRow("End State Discard", "none")
	}
	table.AddRow("github.com/choria-io/asyncjobs", h.AJVersion)
	fmt.Println(table.Render())

	table = newTableWriter("Handler Configuration and Packages")
	table.AddHeaders("Task Type", "Remote", "Package", "Version")
	for _, h := range h.TaskHandlers {
		table.AddRow(h.TaskType, h.RequestReply, h.Package, h.Version)
	}
	fmt.Println(table.Render())
	fmt.Println()

	generator, err := generators.NewGoContainer(h)
	if err != nil {
		return err
	}

	path, err := filepath.Abs(".")
	if err != nil {
		return err
	}

	err = generator.RenderToDirectory(path)
	if err != nil {
		return err
	}

	fmt.Println("Build your container using 'docker build'")

	return nil
}
