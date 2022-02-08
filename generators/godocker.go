// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package generators

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

// GoContainer builds docker containers based on the package spec
type GoContainer struct {
	// Package describes the package to build
	Package *Package
	// BuildTime is when the package is being built, set at runtime
	BuildTime string
}

var (
	//go:embed fs/godocker
	godockerFS embed.FS
)

// NewGoContainer create a new go container builder
func NewGoContainer(handlers *Package) (*GoContainer, error) {
	return &GoContainer{
		Package:   handlers,
		BuildTime: time.Now().Format(time.RFC822),
	}, nil
}

// RenderToDirectory renders the container to a specific directory
func (g *GoContainer) RenderToDirectory(target string) error {
	files, err := godockerFS.ReadDir("fs/godocker")
	if err != nil {
		return err
	}

	for _, p := range g.Package.TaskHandlers {
		if p.Package == "" {
			return fmt.Errorf("task handlers require a package")
		}
		if p.Version == "" {
			return fmt.Errorf("task handlers require a version")
		}
	}

	funcs := map[string]interface{}{
		"TypeToPackageName": func(t string) string {
			remove := []string{"_", "-", ":", "/", "\\"}
			res := t

			for _, r := range remove {
				res = strings.Replace(res, r, "", -1)
			}

			return res
		},
	}

	for _, f := range files {
		if f.IsDir() {
			continue
		}

		t := template.New(f.Name())
		t.Funcs(funcs)
		body, err := godockerFS.ReadFile(filepath.Join("fs/godocker", f.Name()))
		if err != nil {
			return err
		}

		p, err := t.Parse(string(body))
		if err != nil {
			return err
		}

		buf := bytes.NewBuffer([]byte{})

		err = p.Execute(buf, g)
		if err != nil {
			return err
		}

		err = os.WriteFile(filepath.Join(target, strings.TrimSuffix(filepath.Base(f.Name()), ".templ")), buf.Bytes(), 0644)
		if err != nil {
			return err
		}
	}

	return nil
}
