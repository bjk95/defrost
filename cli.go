package main

var CLI struct {
  Exec struct {
    Cmd []string `arg:"" name:"cmd" passthrough:"" help:"Test command to run."`
  } `cmd:"" help:"Execute run."`
}

