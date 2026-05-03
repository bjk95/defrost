package cli

import (
	"runtime"
	"runtime/debug"
)

// Tiny indirection layer around runtime/debug so the wire.go logic
// stays trivially testable. Tests can override these vars to inject
// fake build info.
var (
	debugReadBuildInfo = debug.ReadBuildInfo
	runtimeVersion     = runtime.Version
	runtimeGOOS        = func() string { return runtime.GOOS }
	runtimeGOARCH      = func() string { return runtime.GOARCH }
)
