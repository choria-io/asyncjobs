package api

import (
	"fmt"

	aj "github.com/choria-io/asyncjobs"
)

type Runner interface {
	Execute() (interface{}, error)
}

type AJRunner struct {
	Client *aj.Client
}

func (c AJRunner) Execute() (interface{}, error) {

	fmt.Printf("*** Running EXECUTE \n")
	return nil, nil
}
