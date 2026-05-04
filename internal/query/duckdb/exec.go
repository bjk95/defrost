package duckdb

import "os/exec"

// execCommand is a thin indirection over exec.Command so tests can
// stub git lookups without touching the filesystem.
var execCommand = func(name string, args ...string) *exec.Cmd { return exec.Command(name, args...) }
