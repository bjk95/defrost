package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/bjk95/defrost/internal/golang"
	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/otlp"
	"github.com/bjk95/defrost/internal/persist"
	"github.com/bjk95/defrost/internal/python/pytest"
	"github.com/bjk95/defrost/internal/runner"
)

// defrostVersion is stamped into the Resource attribute service.version.
// Bump when cutting a release; build-time injection can replace this
// constant later.
const defrostVersion = "0.0.0-dev"

// drainGrace is the window we give SDK background flushes after the child
// exits before tearing down the OTLP receiver. See spec defaults table.
const drainGrace = 2 * time.Second

type ExecOpts struct {
	RepoDir    string
	DataBranch string
	Persist    bool
	NoRemote   bool
	Dev        bool
}

func HandleExecution(cmd []string, opts ExecOpts) int {
	if len(cmd) == 0 {
		fmt.Fprintln(os.Stderr, "exec: no command provided")
		return 2
	}

	reg := runner.NewRegistry()
	reg.Register(golang.Adapter{})
	reg.Register(pytest.Adapter{})

	a := reg.Find(cmd)
	if a == nil {
		fmt.Fprintf(os.Stderr, "exec: unsupported test command: %q\n", cmd[0])
		return 2
	}

	pOpts := persist.Options{
		RepoDir:    opts.RepoDir,
		DataBranch: opts.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		NoRemote:   opts.NoRemote,
		Dev:        opts.Dev,
	}

	// Even when Persist is off we still build a RunContext so the receiver
	// can stamp metric data points with a trace_id. Failure here is fatal
	// only when we're going to persist; otherwise we run without metric
	// collection.
	run, runErr := persist.DetectRunContext(pOpts, cmd, defrostVersion)
	if runErr != nil && opts.Persist {
		fmt.Fprintln(os.Stderr, "exec: detect run context:", runErr)
		return 1
	}

	receiver, restoreEnv := startReceiver()
	defer restoreEnv()

	results, code := a.Run(cmd)

	for _, r := range results {
		fmt.Printf("%+v\n", r)
	}

	var metrics []models.MetricEntry
	if receiver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), drainGrace)
		buffered, err := receiver.Shutdown(ctx)
		cancel()
		if err != nil {
			fmt.Fprintln(os.Stderr, "exec: otlp receiver shutdown:", err)
		}
		if runErr == nil {
			for _, req := range buffered {
				metrics = append(metrics, otlp.MetricsToEntries(req, run)...)
			}
		}
	}

	if !opts.Persist || (len(results) == 0 && len(metrics) == 0) {
		return code
	}
	if runErr != nil {
		// Persist requested but no run context. Surface the original error.
		fmt.Fprintln(os.Stderr, "exec: persist skipped, no run context:", runErr)
		if code == 0 {
			code = 1
		}
		return code
	}

	if err := persistRun(pOpts, run, results, metrics, code); err != nil {
		fmt.Fprintln(os.Stderr, "persist: failed:", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

// startReceiver binds the OTLP listener and exports the standard env vars
// for the child. On bind failure we log and return (nil, no-op) so the
// run continues without metric collection. Returns a restore function the
// caller MUST call to clear the exported env vars regardless of outcome.
func startReceiver() (*otlp.Receiver, func()) {

	r := otlp.New()
	port, err := r.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "exec: otlp receiver bind failed, continuing without metrics:", err)
		return nil, func() {}
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	prevEndpoint, hadEndpoint := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT")
	prevProtocol, hadProtocol := os.LookupEnv("OTEL_EXPORTER_OTLP_PROTOCOL")
	os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	os.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	restore := func() {
		if hadEndpoint {
			os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", prevEndpoint)
		} else {
			os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		}
		if hadProtocol {
			os.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", prevProtocol)
		} else {
			os.Unsetenv("OTEL_EXPORTER_OTLP_PROTOCOL")
		}
	}
	return r, restore
}

func persistRun(pOpts persist.Options, run models.RunContext, results []models.TestResult, metrics []models.MetricEntry, exitCode int) error {
	testSpans := otlp.TestResultsToSpans(results, run)

	root := persist.NewRootSpan(run)
	root.EndTimeUnixNano = time.Now().UnixNano()
	root.Status = rootStatusFromExit(exitCode)

	if err := persist.New(pOpts).InsertNewRun(root, testSpans, metrics); err != nil {
		if errors.Is(err, persist.ErrNoOrigin) {
			return errors.New("no 'origin' remote configured. Either add one (`git remote add origin ...`) or pass --no-remote to persist locally only")
		}
		return err
	}
	return nil
}

func rootStatusFromExit(code int) models.SpanStatus {
	if code == 0 {
		return models.SpanStatus{Code: "OK"}
	}
	return models.SpanStatus{Code: "ERROR", Message: fmt.Sprintf("exit code %d", code)}
}
