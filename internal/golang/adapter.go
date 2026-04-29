package golang

// Adapter implements runner.Adapter for `go test ...` invocations.
//
// Matches the literal form `go test [args...]`. Tighter than a prefix-only
// match: `go run`, `go build`, etc. fall through to no-match.
type Adapter struct{}

func (Adapter) Matches(cmd []string) bool {
	return len(cmd) >= 2 && cmd[0] == "go" && cmd[1] == "test"
}

func (Adapter) Run(cmd []string) int {
	return ExecuteGoTest(cmd)
}
