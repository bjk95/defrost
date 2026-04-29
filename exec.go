package main

import (
	"fmt"
	"os"

	"github.com/bjk95/defrost/internal/golang"
	"github.com/bjk95/defrost/internal/python/pytest"
	"github.com/bjk95/defrost/internal/runner"
)

func HandleExecution(cmd []string) {
	reg := runner.NewRegistry()
	reg.Register(golang.Adapter{})
	reg.Register(pytest.Adapter{})

	a := reg.Find(cmd)
	if a == nil {
		fmt.Fprintf(os.Stderr, "defrost: no adapter for %q\n", cmd)
		os.Exit(2)
	}
	os.Exit(a.Run(cmd))
}
