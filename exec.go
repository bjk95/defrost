package main

import (
	"github.com/bjk95/defrost/internal/golang"
)

func HandleExecution(cmd []string) {
	switch cmd[0] {
	case "go":
		golang.ExecuteGoTest(cmd)
	}
}
