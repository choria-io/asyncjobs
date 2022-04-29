package api

import (
	"fmt"

	aj "github.com/choria-io/asyncjobs"
)

type Runner interface {
	Execute() (interface{}, error)
}

type Config struct {
	Context     string
	Queue       string
	Concurrency int
}

type AJRunner struct {
	Client *aj.Client
	Router *aj.Mux
}

func (c AJRunner) Execute() (interface{}, error) {

	fmt.Printf("*** Running EXECUTE \n")
	return nil, nil
}
