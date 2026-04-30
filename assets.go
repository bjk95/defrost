package main

import "embed"

// Assets is the SPA, embedded at build time. The directory is committed
// (rebuilt by CI on web/ changes) so `go install` works without Node.
//
//go:embed all:web/dist
var Assets embed.FS
