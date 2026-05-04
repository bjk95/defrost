// Package assets owns the embedded web bundle. Only `cmd/defrost`
// imports this package; `cmd/defrost-ci` doesn't, so the slim binary
// avoids embedding ~MBs of compiled SPA assets.
package assets

import "embed"

// FS is the dashboard SPA, embedded at build time. The dist directory
// is committed (CI rebuilds it on web/ source changes) so `go install
// github.com/bjk95/defrost/cmd/defrost@latest` works without Node.
//
//go:embed all:dist
var FS embed.FS
