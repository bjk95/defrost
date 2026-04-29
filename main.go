package main

import (
	"fmt"
	"strings"

	"github.com/alecthomas/kong"
)

func main() {
	fmt.Println("Running tests with defrost")
	ctx := kong.Parse(&CLI)
	cmd := ctx.Command()

	switch {
	case strings.HasPrefix(cmd, "exec "):
		HandleExecution(CLI.Exec.Cmd)
	default:
		panic(cmd)
	}
}