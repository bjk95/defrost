# Promptfoo Eval Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Promptfoo support to defrost so `defrost exec promptfoo eval -c <config>` (and the common JS-runtime invocation forms — `npx promptfoo`, `pnpm promptfoo`) wraps a Promptfoo run, parses the resulting JSON, persists per-test pass/fail to `traces/<test_name>.ndjson`, and persists per-assertion scores to `metrics/eval.<assertion-name>.ndjson`. This is step 1 of the eval framework integration build order.

**Architecture:** Slot a fourth runner adapter into the existing `internal/runner` registry. The adapter auto-injects `--output <tempfile>.json` if the user did not supply one, runs `promptfoo eval` to completion, parses Promptfoo's `results.json` shape, and emits both `[]models.TestResult` (one per result row) and `[]*metricspb.Metric` (one gauge per `gradingResult.componentResults[]` entry, each containing exactly one `NumberDataPoint` — same shape `MetricsToEntries` produces on the OTLP path). Eval-specific attributes follow the OTel Gen-AI semconv with a thin `defrost.eval.*` layer for the gaps (threshold). Lands the `runner.Adapter.Run` signature change from `([]TestResult, int)` to `([]TestResult, []*metricspb.Metric, int)`.

**Tech Stack:** Go 1.24 (matches `go.mod`), `encoding/json` (stdlib), `os/exec` (stdlib), `path/filepath` (stdlib), `go.opentelemetry.io/proto/otlp/metrics/v1` and `.../common/v1` (already in `go.mod` — see `go.sum`). No new third-party Go dependencies. Examples use `promptfoo@^0.x` from npm.

**Spec:** [docs/superpowers/specs/2026-04-30-eval-framework-integration-design.md](docs/superpowers/specs/2026-04-30-eval-framework-integration-design.md)

## Prerequisites

The **OTel-Aligned Storage and Metrics** PR (#7) is merged on `main` as of 2026-04-30. Before executing Task 1, ensure this branch has been merged with `origin/main` so the following are present:

- `internal/models/runcontext.go` with `RunContext`, `StringAttr`, `IntAttr`, `BoolAttr`, `NewSpanID`.
- `internal/otlp/translate.go` with `TestResultsToSpans` and `MetricsToEntries`.
- `internal/persist/persist.go` with `Backend.InsertNewRun(traces, metrics)`, `WrapSpansInResource`, `WrapMetricsInResource`, `MetricResource`, `NewRootSpan`.
- `exec.go` already has the `persistRun(pOpts, run, results, metrics, exitCode)` helper that takes `metrics []*metricspb.Metric` from the OTLP receiver path. This plan adds a *second source* for that slice (adapter-emitted metrics), to be merged in alongside receiver-emitted metrics.

Run `git status` and confirm `internal/otlp/`, `internal/models/runcontext.go`, and the proto imports in `exec.go` are present. If they're not, merge `origin/main` first.

---

## File Structure

| Path | Status | Responsibility |
|---|---|---|
| `internal/runner/adapter.go` | edit | `Run` signature changes to return `([]TestResult, []*metricspb.Metric, int)` |
| `internal/runner/registry_test.go` | edit | `fakeAdapter.Run` matches new signature |
| `internal/golang/adapter.go` (or sibling files) | edit | Returns `nil` for the new metrics slot |
| `internal/python/pytest/adapter.go` | edit | Returns `nil` for the new metrics slot |
| `internal/python/pytest/adapter_test.go` | edit | Stub adapters / call sites match the new shape |
| `internal/javascript/jest/adapter.go` | edit | Returns `nil` for the new metrics slot |
| `internal/javascript/jest/adapter_test.go` | edit | Stub adapters / call sites match the new shape |
| `internal/models/runcontext.go` | edit | Add a `DoubleAttr` helper alongside the existing `StringAttr`/`IntAttr`/`BoolAttr` |
| `internal/models/runcontext_test.go` | edit | One unit test covering `DoubleAttr` |
| `exec.go` | edit | Captures adapter-emitted metrics from the new `Run` slot; merges with the receiver-emitted slice already in `persistRun`; registers the new adapter |
| `exec_test.go` | edit | `stubAdapter.Run` matches new signature; one new test asserts adapter-emitted metrics reach `persistRun` |
| `internal/eval/promptfoo/parser.go` | new | Promptfoo `results.json` → `([]TestResult, []*metricspb.Metric)` (pure) |
| `internal/eval/promptfoo/parser_test.go` | new | Table-driven parser tests over fixtures, asserting proto field values |
| `internal/eval/promptfoo/adapter.go` | new | Matcher (pure-argv) + Run flow + arg injection helpers |
| `internal/eval/promptfoo/adapter_test.go` | new | Table-driven matcher tests + arg-injection tests |
| `internal/eval/promptfoo/testdata/single_assertion.json` | new | One result, one assertion |
| `internal/eval/promptfoo/testdata/multi_assertion.json` | new | One result, three assertions (one fails) |
| `internal/eval/promptfoo/testdata/multi_test.json` | new | Three results, mixed pass/fail |
| `internal/eval/promptfoo/testdata/with_threshold.json` | new | Assertion with threshold |
| `internal/eval/promptfoo/testdata/with_metric_override.json` | new | Assertion with custom `metric` name overriding `type` |
| `internal/eval/promptfoo/testdata/empty.json` | new | `results.results: []` |
| `examples/promptfoo/promptfooconfig.yaml` | new | Tiny eval (one prompt, two assertions) |
| `examples/promptfoo/.gitignore` | new | Ignores `node_modules/`, `output.json`, defrost dev artefacts |
| `examples/promptfoo/README.md` | new | One-paragraph setup note |
| `.github/workflows/integration.yml` | edit | Add `promptfoo` job |

---

## Task 1: Extend `runner.Adapter` interface signature

**Files:**
- Modify: `internal/runner/adapter.go`
- Modify: `internal/runner/registry_test.go`

The `Run` signature gains a third return value: `[]*metricspb.Metric`. Existing adapters return `nil` for the new slot. This is a mechanical, contained change confined to one interface plus its test fakes.

- [ ] **Step 1: Update the interface in `internal/runner/adapter.go`**

Replace the existing `Adapter` interface:

```go
package runner

import (
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

	"github.com/bjk95/defrost/internal/models"
)

// Adapter wraps a single language/test-framework integration. Implementations
// inspect a defrost-exec argv and decide whether they handle it (Matches),
// then run the underlying child command and return the parsed test results,
// any framework-emitted eval/score metric data points, and the child's exit
// code (Run). Adapters that don't emit metrics return a nil slice.
//
// Each *metricspb.Metric in the returned slice MUST contain exactly one
// data point (gauge / sum / histogram). This matches the convention
// established by `otlp.MetricsToEntries` for receiver-emitted metrics —
// the persistence layer writes one line per *metricspb.Metric.
type Adapter interface {
	Matches(cmd []string) bool
	Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int)
}
```

- [ ] **Step 2: Run go build to find every call site**

Run: `go build ./...`
Expected: compile errors in `internal/runner/registry_test.go`, `internal/golang/`, `internal/python/pytest/`, `internal/javascript/jest/`, `exec.go`, `exec_test.go` — every implementation of `Adapter` and every caller of `Run`.

- [ ] **Step 3: Update `fakeAdapter` in `internal/runner/registry_test.go`**

Find the existing `fakeAdapter.Run` method and update its signature:

```go
import (
	"testing"

	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

	"github.com/bjk95/defrost/internal/models"
)

func (a fakeAdapter) Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int) {
	return nil, nil, a.exit
}
```

(Merge the `metricspb` import into the existing import block.)

- [ ] **Step 4: Run the registry tests**

Run: `go test ./internal/runner/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runner/adapter.go internal/runner/registry_test.go
git commit -m "feat(runner): Adapter.Run returns *metricspb.Metric slice"
```

---

## Task 2: Update existing runner adapters to new signature

**Files:**
- Modify: `internal/golang/adapter.go` (locate the `Run` method via `grep -n "func.*Run.*[]models.TestResult" internal/golang/`)
- Modify: `internal/python/pytest/adapter.go`
- Modify: `internal/python/pytest/adapter_test.go`
- Modify: `internal/javascript/jest/adapter.go`
- Modify: `internal/javascript/jest/adapter_test.go`

Each existing adapter's `Run` returns the same `[]TestResult, exit` it did before, plus a `nil` slice for metrics. Test files that mock `Adapter` follow.

- [ ] **Step 1: Update `internal/golang/adapter.go`**

Locate the `Run` method (`grep -n "func.*Run.*[]models.TestResult" internal/golang/`). Add the `metricspb` import to the file's import block, then change the signature and every `return ...` statement so `nil` is inserted in the new metrics slot.

For example:

```go
// before
func (a Adapter) Run(cmd []string) ([]models.TestResult, int) {
    ...
    return results, exitCode
}
```

```go
// after
import (
    // ...existing imports
    metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

func (a Adapter) Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int) {
    ...
    return results, nil, exitCode
}
```

Update every error-path return in the same function (e.g. `return nil, 1` → `return nil, nil, 1`).

- [ ] **Step 2: Run go test for the golang adapter**

Run: `go test ./internal/golang/...`
Expected: PASS.

- [ ] **Step 3: Update `internal/python/pytest/adapter.go`**

Same pattern — find `func.*Run.*[]models.TestResult` in `internal/python/pytest/adapter.go`, add the `metricspb` import, change the signature and every `return ...` in the function body so `nil` is inserted in the new metrics slot.

- [ ] **Step 4: Update `internal/python/pytest/adapter_test.go`**

Run: `go vet ./internal/python/pytest/...`
Expected: every compile error names a `Run(...)` call site or stub. For each error, locate the line and:
- If it's a stub adapter mocking `Run`, update the method signature to return `(results, nil, code)`.
- If it's a call site reading `Run(cmd)`'s return, update `_, _ := a.Run(cmd)` to accept three values.

- [ ] **Step 5: Run pytest adapter tests**

Run: `go test ./internal/python/pytest/...`
Expected: PASS.

- [ ] **Step 6: Update `internal/javascript/jest/adapter.go`**

Same as Step 1 — `internal/javascript/jest/adapter.go` already shows the existing `Run` method (`grep -n "func.*Adapter.*Run" internal/javascript/jest/adapter.go`). Add the `metricspb` import, change the signature, change every `return ...`.

```go
// before
func (a *Adapter) Run(cmd []string) ([]models.TestResult, int) {
    if hasUserJSONFlag(cmd) {
        ...
        return nil, 2
    }
    ...
    return results, exitCode
}
```

```go
// after
func (a *Adapter) Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int) {
    if hasUserJSONFlag(cmd) {
        ...
        return nil, nil, 2
    }
    ...
    return results, nil, exitCode
}
```

- [ ] **Step 7: Update `internal/javascript/jest/adapter_test.go`**

Same pattern as Step 4: any test that mocks `Run` or reads its return tuple needs the `nil`-metrics slot.

- [ ] **Step 8: Run jest adapter tests**

Run: `go test ./internal/javascript/jest/...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/golang/ internal/python/pytest/ internal/javascript/jest/
git commit -m "feat(adapters): existing runners return nil metrics under new Adapter signature"
```

---

## Task 3: Plumb adapter metrics into `exec.go`'s persistence path

**Files:**
- Modify: `exec.go`
- Modify: `exec_test.go`

`exec.go` already has a `metrics []*metricspb.Metric` slice that accumulates receiver-emitted metrics before calling `persistRun`. This task captures the adapter's new return slot and merges it into that same slice.

- [ ] **Step 1: Update `stubAdapter` in `exec_test.go`**

The existing stub at `exec_test.go` (around line 14) looks like:

```go
type stubAdapter struct {
	results []models.TestResult
	code    int
}

func (s stubAdapter) Matches(cmd []string) bool { return cmd[0] == "stub" }
func (s stubAdapter) Run(cmd []string) ([]models.TestResult, int) {
	return s.results, s.code
}
```

Add a `metrics` field, update the signature, and add the `metricspb` import:

```go
import (
	// ...existing imports
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

type stubAdapter struct {
	results []models.TestResult
	metrics []*metricspb.Metric
	code    int
}

func (s stubAdapter) Matches(cmd []string) bool { return cmd[0] == "stub" }
func (s stubAdapter) Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int) {
	return s.results, s.metrics, s.code
}
```

- [ ] **Step 2: Update the call site in `exec.go`**

`exec.go` currently has (around line 102):

```go
results, code := a.Run(cmd)
```

Change to:

```go
results, adapterMetrics, code := a.Run(cmd)
```

Then, in the block that already accumulates receiver metrics (around line 109-120, the `var metrics []*metricspb.Metric` block), prepend the adapter-emitted metrics so they reach `persistRun`:

```go
var metrics []*metricspb.Metric
metrics = append(metrics, adapterMetrics...)
if receiver != nil {
	ctx, cancel := context.WithTimeout(context.Background(), drainGrace)
	buffered, err := receiver.Shutdown(ctx)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "exec: otlp receiver shutdown:", err)
	}
	for _, req := range buffered {
		metrics = append(metrics, otlp.MetricsToEntries(req, run)...)
	}
}
```

The rest of `persistRun` and exit-code logic is unchanged.

- [ ] **Step 3: Add a focused test that adapter metrics reach persistence**

Append to `exec_test.go`:

```go
func TestExecPlumbsAdapterMetricsToPersistence(t *testing.T) {
	repo := makeRepo(t)
	pOpts := persist.Options{RepoDir: repo, NoRemote: true}

	results := []models.TestResult{{Id: "x.y.Z", Ran: true, Passed: true, Duration: time.Millisecond}}
	score := 0.87
	now := uint64(time.Now().UnixNano())
	metrics := []*metricspb.Metric{{
		Name: "eval.faithfulness",
		Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
			DataPoints: []*metricspb.NumberDataPoint{{
				TimeUnixNano: now,
				Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: score},
				Attributes: []*commonpb.KeyValue{
					models.StringAttr("test.case.name", "x.y.Z"),
					models.StringAttr("gen_ai.evaluation.name", "faithfulness"),
				},
			}},
		}},
	}}
	a := stubAdapter{results: results, metrics: metrics, code: 0}

	code := execWith(a, []string{"stub"}, ExecOpts{
		RepoDir:  repo,
		Persist:  true,
		NoRemote: true,
	})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	// Read back the metric from the data branch.
	got, err := persist.New(pOpts).GetMetricHistory("eval.faithfulness")
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one persisted metric, got %d", len(got))
	}
}
```

Add the imports (merge into the existing `import (...)` block):

```go
import (
	// ...existing imports
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
)
```

If `persist.GetMetricHistory` is named differently in the merged-from-main branch (the OTel storage spec calls the read API `GetTestHistory` for spans; the metrics-side analog may be `GetMetricHistory`, `LoadMetrics`, or similar — check `internal/persist/persist.go`), use whatever read API is actually exposed. The test's contract is "the metric round-trips through `execWith`"; the exact API name follows what shipped.

- [ ] **Step 4: Run the exec tests**

Run: `go test -run TestExec ./...`
Expected: PASS.

- [ ] **Step 5: Run the full suite to confirm no regressions**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add exec.go exec_test.go
git commit -m "feat(exec): merge adapter-emitted metrics into persistence path"
```

---

## Task 4: Promptfoo JSON fixtures

**Files:**
- Create: `internal/eval/promptfoo/testdata/single_assertion.json`
- Create: `internal/eval/promptfoo/testdata/multi_assertion.json`
- Create: `internal/eval/promptfoo/testdata/multi_test.json`
- Create: `internal/eval/promptfoo/testdata/with_threshold.json`
- Create: `internal/eval/promptfoo/testdata/with_metric_override.json`
- Create: `internal/eval/promptfoo/testdata/empty.json`

These are minimal JSON documents matching what `promptfoo eval --output results.json` writes. Only the fields the parser reads are populated. Field names are from the verified Promptfoo output schema:

- `results.results[].success` (bool)
- `results.results[].response.output` (string)
- `results.results[].provider.label` (string)
- `results.results[].vars` (map; used to synthesise `test.case.name`)
- `results.results[].gradingResult.componentResults[].pass` (bool)
- `results.results[].gradingResult.componentResults[].score` (number)
- `results.results[].gradingResult.componentResults[].reason` (string)
- `results.results[].gradingResult.componentResults[].assertion.type` (string)
- `results.results[].gradingResult.componentResults[].assertion.threshold` (number, optional)
- `results.results[].gradingResult.componentResults[].assertion.metric` (string, optional — overrides `type` when present)

- [ ] **Step 1: Create `single_assertion.json`**

```json
{
  "results": {
    "results": [
      {
        "success": true,
        "response": { "output": "The capital of France is Paris." },
        "provider": { "label": "openai:gpt-4o" },
        "vars": { "country": "France" },
        "gradingResult": {
          "pass": true,
          "score": 1.0,
          "reason": "Output matches expected.",
          "componentResults": [
            {
              "pass": true,
              "score": 1.0,
              "reason": "Contains 'Paris'",
              "assertion": { "type": "contains", "value": "Paris" }
            }
          ]
        }
      }
    ]
  }
}
```

- [ ] **Step 2: Create `multi_assertion.json`**

```json
{
  "results": {
    "results": [
      {
        "success": false,
        "response": { "output": "The answer is 42, citing Hitchhiker's Guide." },
        "provider": { "label": "anthropic:claude-sonnet-4-6" },
        "vars": { "question": "meaning of life" },
        "gradingResult": {
          "pass": false,
          "score": 0.66,
          "reason": "2/3 assertions passed",
          "componentResults": [
            {
              "pass": true,
              "score": 1.0,
              "reason": "Contains '42'",
              "assertion": { "type": "contains", "value": "42" }
            },
            {
              "pass": true,
              "score": 1.0,
              "reason": "Output is concise",
              "assertion": { "type": "llm-rubric", "value": "Concise (<200 chars)" }
            },
            {
              "pass": false,
              "score": 0.0,
              "reason": "Missing source citation",
              "assertion": { "type": "factuality", "value": "Cites a source" }
            }
          ]
        }
      }
    ]
  }
}
```

- [ ] **Step 3: Create `multi_test.json`**

```json
{
  "results": {
    "results": [
      {
        "success": true,
        "response": { "output": "Paris" },
        "provider": { "label": "openai:gpt-4o" },
        "vars": { "country": "France" },
        "gradingResult": {
          "pass": true, "score": 1.0, "reason": "ok",
          "componentResults": [
            { "pass": true, "score": 1.0, "reason": "contains Paris", "assertion": { "type": "contains" } }
          ]
        }
      },
      {
        "success": false,
        "response": { "output": "I don't know" },
        "provider": { "label": "openai:gpt-4o" },
        "vars": { "country": "Spain" },
        "gradingResult": {
          "pass": false, "score": 0.0, "reason": "fail",
          "componentResults": [
            { "pass": false, "score": 0.0, "reason": "missing Madrid", "assertion": { "type": "contains" } }
          ]
        }
      },
      {
        "success": true,
        "response": { "output": "Berlin" },
        "provider": { "label": "openai:gpt-4o" },
        "vars": { "country": "Germany" },
        "gradingResult": {
          "pass": true, "score": 1.0, "reason": "ok",
          "componentResults": [
            { "pass": true, "score": 1.0, "reason": "contains Berlin", "assertion": { "type": "contains" } }
          ]
        }
      }
    ]
  }
}
```

- [ ] **Step 4: Create `with_threshold.json`**

```json
{
  "results": {
    "results": [
      {
        "success": true,
        "response": { "output": "Paris" },
        "provider": { "label": "openai:gpt-4o" },
        "vars": { "country": "France" },
        "gradingResult": {
          "pass": true, "score": 0.92, "reason": "ok",
          "componentResults": [
            {
              "pass": true,
              "score": 0.92,
              "reason": "Above threshold",
              "assertion": { "type": "similar", "value": "Paris", "threshold": 0.85 }
            }
          ]
        }
      }
    ]
  }
}
```

- [ ] **Step 5: Create `with_metric_override.json`**

```json
{
  "results": {
    "results": [
      {
        "success": true,
        "response": { "output": "Paris is the capital of France." },
        "provider": { "label": "openai:gpt-4o" },
        "vars": { "country": "France" },
        "gradingResult": {
          "pass": true, "score": 1.0, "reason": "ok",
          "componentResults": [
            {
              "pass": true,
              "score": 1.0,
              "reason": "Highly relevant to user's query",
              "assertion": { "type": "llm-rubric", "value": "Relevant", "metric": "relevance" }
            }
          ]
        }
      }
    ]
  }
}
```

- [ ] **Step 6: Create `empty.json`**

```json
{ "results": { "results": [] } }
```

- [ ] **Step 7: Commit fixtures**

```bash
git add internal/eval/promptfoo/testdata/
git commit -m "test(promptfoo): add results.json fixtures for parser tests"
```

---

## Task 5: Promptfoo parser

**Files:**
- Modify: `internal/models/runcontext.go` (add `DoubleAttr` helper)
- Modify: `internal/models/runcontext_test.go` (one unit test for the helper)
- Create: `internal/eval/promptfoo/parser.go`
- Create: `internal/eval/promptfoo/parser_test.go`

Pure function: `Parse(r io.Reader) ([]models.TestResult, []*metricspb.Metric, error)`. Each `results.results[i]` produces:

- One `TestResult` with `Id` synthesised from `vars` (sorted-key concatenation, e.g. `country=France`), `Passed` from `success`, `Output` from `response.output` plus joined assertion reasons on failure.
- One `*metricspb.Metric` (gauge, single `NumberDataPoint`) per `componentResults[k]` with metric name `eval.<assertion.metric>` if `metric` is set, else `eval.<assertion.type>`. Score in `NumberDataPoint.Value` as `AsDouble`. Attributes carry `gen_ai.evaluation.*` per the spec, plus `defrost.eval.threshold` when present.

- [ ] **Step 1: Add `DoubleAttr` helper to `internal/models/runcontext.go`**

Append to the existing helpers block (just below `IntAttr`):

```go
// DoubleAttr returns a *commonpb.KeyValue carrying a float64.
func DoubleAttr(key string, value float64) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: value}},
	}
}
```

- [ ] **Step 2: Add a unit test for `DoubleAttr`**

Append to `internal/models/runcontext_test.go`:

```go
func TestDoubleAttr(t *testing.T) {
	kv := DoubleAttr("eval.score", 0.87)
	if kv.Key != "eval.score" {
		t.Fatalf("expected key eval.score, got %q", kv.Key)
	}
	dv, ok := kv.Value.Value.(*commonpb.AnyValue_DoubleValue)
	if !ok {
		t.Fatalf("expected DoubleValue payload, got %T", kv.Value.Value)
	}
	if dv.DoubleValue != 0.87 {
		t.Fatalf("expected 0.87, got %v", dv.DoubleValue)
	}
}
```

(The test file already imports `commonpb` for the existing `String/Int/Bool` tests; if not, add `commonpb "go.opentelemetry.io/proto/otlp/common/v1"`.)

- [ ] **Step 3: Run the helper test**

Run: `go test ./internal/models/...`
Expected: PASS.

- [ ] **Step 4: Write a failing parser test**

Create `internal/eval/promptfoo/parser_test.go`:

```go
package promptfoo

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

// gaugeValue returns the AsDouble value of a single-data-point gauge metric.
// Fails the test if m isn't a gauge with exactly one numeric data point.
func gaugeValue(t *testing.T, m *metricspb.Metric) float64 {
	t.Helper()
	g, ok := m.Data.(*metricspb.Metric_Gauge)
	if !ok {
		t.Fatalf("expected gauge, got %T", m.Data)
	}
	if len(g.Gauge.DataPoints) != 1 {
		t.Fatalf("expected 1 data point, got %d", len(g.Gauge.DataPoints))
	}
	dp := g.Gauge.DataPoints[0]
	dv, ok := dp.Value.(*metricspb.NumberDataPoint_AsDouble)
	if !ok {
		t.Fatalf("expected AsDouble, got %T", dp.Value)
	}
	return dv.AsDouble
}

// attrString returns the string value of the named attribute on the gauge's
// data point, or "" if missing or non-string.
func attrString(m *metricspb.Metric, key string) string {
	g, _ := m.Data.(*metricspb.Metric_Gauge)
	if g == nil || len(g.Gauge.DataPoints) == 0 {
		return ""
	}
	for _, kv := range g.Gauge.DataPoints[0].Attributes {
		if kv.Key == key {
			if sv, ok := kv.Value.Value.(*commonpb.AnyValue_StringValue); ok {
				return sv.StringValue
			}
		}
	}
	return ""
}

// attrDouble returns the double value of the named attribute on the gauge's
// data point, plus a bool indicating presence.
func attrDouble(m *metricspb.Metric, key string) (float64, bool) {
	g, _ := m.Data.(*metricspb.Metric_Gauge)
	if g == nil || len(g.Gauge.DataPoints) == 0 {
		return 0, false
	}
	for _, kv := range g.Gauge.DataPoints[0].Attributes {
		if kv.Key == key {
			if dv, ok := kv.Value.Value.(*commonpb.AnyValue_DoubleValue); ok {
				return dv.DoubleValue, true
			}
		}
	}
	return 0, false
}

func TestParseSingleAssertion(t *testing.T) {
	tests, metrics, err := Parse(bytes.NewReader(loadFixture(t, "single_assertion.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("expected 1 test result, got %d", len(tests))
	}
	if !tests[0].Passed {
		t.Fatalf("expected Passed=true, got false")
	}
	if tests[0].Id != "country=France" {
		t.Fatalf("expected Id=country=France, got %q", tests[0].Id)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	m := metrics[0]
	if m.Name != "eval.contains" {
		t.Fatalf("expected metric name eval.contains, got %q", m.Name)
	}
	if got := gaugeValue(t, m); got != 1.0 {
		t.Fatalf("expected score 1.0, got %v", got)
	}
	if got := attrString(m, "gen_ai.evaluation.name"); got != "contains" {
		t.Fatalf("expected gen_ai.evaluation.name=contains, got %q", got)
	}
	if got := attrString(m, "gen_ai.evaluation.score.label"); got != "pass" {
		t.Fatalf("expected gen_ai.evaluation.score.label=pass, got %q", got)
	}
	if got := attrString(m, "test.case.name"); got != "country=France" {
		t.Fatalf("expected test.case.name=country=France, got %q", got)
	}
	if got := attrString(m, "gen_ai.request.model"); got != "openai:gpt-4o" {
		t.Fatalf("expected gen_ai.request.model=openai:gpt-4o, got %q", got)
	}
}
```

- [ ] **Step 5: Run the test to verify it fails**

Run: `go test ./internal/eval/promptfoo/...`
Expected: FAIL — `parser.go` does not exist; `Parse` is undefined.

- [ ] **Step 6: Implement `Parse` in `internal/eval/promptfoo/parser.go`**

```go
package promptfoo

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

	"github.com/bjk95/defrost/internal/models"
)

// promptfooDoc is the top-level JSON shape `promptfoo eval --output X.json`
// writes. We decode only the fields we use.
type promptfooDoc struct {
	Results struct {
		Results []promptfooResult `json:"results"`
	} `json:"results"`
}

type promptfooResult struct {
	Success       bool                  `json:"success"`
	Response      promptfooResponse     `json:"response"`
	Provider      promptfooProvider     `json:"provider"`
	Vars          map[string]any        `json:"vars"`
	GradingResult promptfooGradingShape `json:"gradingResult"`
}

type promptfooResponse struct {
	Output string `json:"output"`
}

type promptfooProvider struct {
	Label string `json:"label"`
	ID    string `json:"id"`
}

type promptfooGradingShape struct {
	Pass             bool                       `json:"pass"`
	Score            float64                    `json:"score"`
	Reason           string                     `json:"reason"`
	ComponentResults []promptfooComponentResult `json:"componentResults"`
}

type promptfooComponentResult struct {
	Pass      bool               `json:"pass"`
	Score     float64            `json:"score"`
	Reason    string             `json:"reason"`
	Assertion promptfooAssertion `json:"assertion"`
}

type promptfooAssertion struct {
	Type      string   `json:"type"`
	Value     any      `json:"value"`
	Threshold *float64 `json:"threshold,omitempty"`
	Metric    string   `json:"metric,omitempty"`
}

// Parse reads a `promptfoo eval --output X.json` document and emits the
// per-result TestResult plus one *metricspb.Metric (gauge, single data
// point) per assertion's componentResult.
//
// Returns nil/nil/error only on JSON decode failure.
func Parse(r io.Reader) ([]models.TestResult, []*metricspb.Metric, error) {
	var doc promptfooDoc
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, nil, fmt.Errorf("parse promptfoo json: %w", err)
	}
	now := uint64(time.Now().UnixNano())
	var (
		tests   []models.TestResult
		metrics []*metricspb.Metric
	)
	for _, r := range doc.Results.Results {
		caseName := caseName(r.Vars)
		tests = append(tests, mapResult(r, caseName))
		for _, c := range r.GradingResult.ComponentResults {
			metrics = append(metrics, mapComponentResult(c, caseName, providerLabel(r.Provider), now))
		}
	}
	return tests, metrics, nil
}

func caseName(vars map[string]any) string {
	if len(vars) == 0 {
		return "<unnamed>"
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, vars[k]))
	}
	return strings.Join(parts, ",")
}

func providerLabel(p promptfooProvider) string {
	if p.Label != "" {
		return p.Label
	}
	return p.ID
}

func mapResult(r promptfooResult, caseName string) models.TestResult {
	output := r.Response.Output
	if !r.Success {
		var fails []string
		for _, c := range r.GradingResult.ComponentResults {
			if !c.Pass {
				fails = append(fails, fmt.Sprintf("[%s] %s", assertionMetricName(c.Assertion), c.Reason))
			}
		}
		if len(fails) > 0 {
			output = output + "\n--- defrost: failed assertions ---\n" + strings.Join(fails, "\n")
		}
	}
	return models.TestResult{
		Id:     caseName,
		Ran:    true,
		Passed: r.Success,
		Output: output,
	}
}

func mapComponentResult(c promptfooComponentResult, caseName, model string, timeUnixNano uint64) *metricspb.Metric {
	criterion := assertionMetricName(c.Assertion)
	score := c.Score

	attrs := []*commonpb.KeyValue{
		models.StringAttr("gen_ai.evaluation.name", criterion),
		models.DoubleAttr("gen_ai.evaluation.score.value", score),
		models.StringAttr("gen_ai.evaluation.score.label", passFailLabel(c.Pass)),
		models.StringAttr("gen_ai.evaluation.explanation", c.Reason),
		models.StringAttr("test.case.name", caseName),
	}
	if model != "" {
		attrs = append(attrs, models.StringAttr("gen_ai.request.model", model))
	}
	if c.Assertion.Threshold != nil {
		attrs = append(attrs, models.DoubleAttr("defrost.eval.threshold", *c.Assertion.Threshold))
	}

	return &metricspb.Metric{
		Name: "eval." + criterion,
		Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
			DataPoints: []*metricspb.NumberDataPoint{{
				TimeUnixNano: timeUnixNano,
				Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: score},
				Attributes:   attrs,
			}},
		}},
	}
}

func assertionMetricName(a promptfooAssertion) string {
	if a.Metric != "" {
		return a.Metric
	}
	return a.Type
}

func passFailLabel(pass bool) string {
	if pass {
		return "pass"
	}
	return "fail"
}
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `go test ./internal/eval/promptfoo/...`
Expected: PASS for `TestParseSingleAssertion`.

- [ ] **Step 8: Add tests for the remaining fixtures**

Append to `internal/eval/promptfoo/parser_test.go`:

```go
func TestParseMultiAssertion(t *testing.T) {
	tests, metrics, err := Parse(bytes.NewReader(loadFixture(t, "multi_assertion.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(tests))
	}
	if tests[0].Passed {
		t.Fatalf("expected Passed=false")
	}
	if !strings.Contains(tests[0].Output, "Missing source citation") {
		t.Fatalf("expected failure reason in Output, got %q", tests[0].Output)
	}
	if len(metrics) != 3 {
		t.Fatalf("expected 3 metrics, got %d", len(metrics))
	}
	wantNames := map[string]bool{"eval.contains": false, "eval.llm-rubric": false, "eval.factuality": false}
	for _, m := range metrics {
		wantNames[m.Name] = true
	}
	for k, v := range wantNames {
		if !v {
			t.Fatalf("missing expected metric name %s", k)
		}
	}
}

func TestParseMultiTest(t *testing.T) {
	tests, metrics, err := Parse(bytes.NewReader(loadFixture(t, "multi_test.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 3 {
		t.Fatalf("expected 3 tests, got %d", len(tests))
	}
	if len(metrics) != 3 {
		t.Fatalf("expected 3 metrics (one per test, one assertion each), got %d", len(metrics))
	}
	pass, fail := 0, 0
	for _, tr := range tests {
		if tr.Passed {
			pass++
		} else {
			fail++
		}
	}
	if pass != 2 || fail != 1 {
		t.Fatalf("expected 2 pass / 1 fail, got %d/%d", pass, fail)
	}
}

func TestParseWithThreshold(t *testing.T) {
	_, metrics, err := Parse(bytes.NewReader(loadFixture(t, "with_threshold.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	got, ok := attrDouble(metrics[0], "defrost.eval.threshold")
	if !ok {
		t.Fatalf("expected defrost.eval.threshold attribute")
	}
	if got != 0.85 {
		t.Fatalf("expected threshold=0.85, got %v", got)
	}
}

func TestParseWithMetricOverride(t *testing.T) {
	_, metrics, err := Parse(bytes.NewReader(loadFixture(t, "with_metric_override.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].Name != "eval.relevance" {
		t.Fatalf("expected name eval.relevance (from metric override), got %q", metrics[0].Name)
	}
}

func TestParseEmpty(t *testing.T) {
	tests, metrics, err := Parse(bytes.NewReader(loadFixture(t, "empty.json")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tests) != 0 || len(metrics) != 0 {
		t.Fatalf("expected zero results, got %d tests / %d metrics", len(tests), len(metrics))
	}
}
```

Add the import `"strings"` to the existing import block at the top of `parser_test.go`.

- [ ] **Step 9: Run all parser tests**

Run: `go test ./internal/eval/promptfoo/... -v`
Expected: every test PASSES.

- [ ] **Step 10: Commit**

```bash
git add internal/models/runcontext.go internal/models/runcontext_test.go internal/eval/promptfoo/parser.go internal/eval/promptfoo/parser_test.go
git commit -m "feat(promptfoo): parse results.json into TestResults + *metricspb.Metric"
```

---

## Task 6: Promptfoo adapter — Matches and arg injection

**Files:**
- Create: `internal/eval/promptfoo/adapter.go`
- Create: `internal/eval/promptfoo/adapter_test.go`

The matcher recognises the literal `promptfoo` token followed by `eval`, plus the wrapper forms `npx promptfoo eval ...` and `pnpm promptfoo eval ...`. The arg-injection helper appends `--output <tempfile>.json` if the user did not already pass `--output` / `-o` / `--output=<path>`.

- [ ] **Step 1: Write failing matcher tests**

Create `internal/eval/promptfoo/adapter_test.go`. The plan's later tasks
will add more tests to this file, so the import block uses the
parenthesised form from the start:

```go
package promptfoo

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// silence unused imports until later tasks introduce real uses
var _ = io.Discard
var _ = exec.Command
var _ = os.CreateTemp
var _ = filepath.Join
var _ = runtime.GOOS

func TestMatches(t *testing.T) {
	cases := []struct {
		name string
		cmd  []string
		want bool
	}{
		{"direct", []string{"promptfoo", "eval"}, true},
		{"direct with config", []string{"promptfoo", "eval", "-c", "promptfooconfig.yaml"}, true},
		{"npx", []string{"npx", "promptfoo", "eval"}, true},
		{"npx with -y", []string{"npx", "-y", "promptfoo", "eval"}, true},
		{"npx with @latest", []string{"npx", "promptfoo@latest", "eval"}, true},
		{"pnpm", []string{"pnpm", "promptfoo", "eval"}, true},
		{"pnpm dlx", []string{"pnpm", "dlx", "promptfoo", "eval"}, true},
		{"yarn", []string{"yarn", "promptfoo", "eval"}, true},
		{"missing eval subcommand", []string{"promptfoo"}, false},
		{"different subcommand", []string{"promptfoo", "view"}, false},
		{"unrelated cmd", []string{"jest"}, false},
		{"empty", []string{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Adapter{}
			got := a.Matches(tc.cmd)
			if got != tc.want {
				t.Fatalf("Matches(%v) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestBuildArgsInjectsOutputFlag(t *testing.T) {
	args := buildArgs([]string{"promptfoo", "eval", "-c", "promptfooconfig.yaml"}, "/tmp/results.json")
	want := []string{"eval", "-c", "promptfooconfig.yaml", "--output", "/tmp/results.json"}
	if !equalSlices(args, want) {
		t.Fatalf("buildArgs = %v, want %v", args, want)
	}
}

func TestBuildArgsRespectsUserLongFlag(t *testing.T) {
	args := buildArgs([]string{"promptfoo", "eval", "--output", "user.json"}, "/tmp/results.json")
	want := []string{"eval", "--output", "user.json"}
	if !equalSlices(args, want) {
		t.Fatalf("buildArgs = %v, want %v", args, want)
	}
}

func TestBuildArgsRespectsUserShortFlag(t *testing.T) {
	args := buildArgs([]string{"promptfoo", "eval", "-o", "user.json"}, "/tmp/results.json")
	want := []string{"eval", "-o", "user.json"}
	if !equalSlices(args, want) {
		t.Fatalf("buildArgs = %v, want %v", args, want)
	}
}

func TestBuildArgsRespectsLongFlagWithEquals(t *testing.T) {
	args := buildArgs([]string{"promptfoo", "eval", "--output=user.json"}, "/tmp/results.json")
	want := []string{"eval", "--output=user.json"}
	if !equalSlices(args, want) {
		t.Fatalf("buildArgs = %v, want %v", args, want)
	}
}

func TestUserOutputPath(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"eval", "--output", "user.json"}, "user.json"},
		{[]string{"eval", "-o", "user.json"}, "user.json"},
		{[]string{"eval", "--output=user.json"}, "user.json"},
		{[]string{"eval", "-c", "x.yaml"}, ""},
	}
	for _, tc := range cases {
		got := userOutputPath(tc.args)
		if got != tc.want {
			t.Fatalf("userOutputPath(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/eval/promptfoo/...`
Expected: FAIL — `Adapter`, `buildArgs`, `userOutputPath` undefined.

- [ ] **Step 3: Implement `internal/eval/promptfoo/adapter.go`**

```go
package promptfoo

import (
	"strings"
)

// Adapter implements runner.Adapter for `promptfoo eval` invocations.
// Recognised forms:
//
//   - direct:    promptfoo eval ...
//   - npx:       npx promptfoo eval ..., npx -y promptfoo eval ..., npx promptfoo@latest eval ...
//   - pnpm:      pnpm promptfoo eval ..., pnpm dlx promptfoo eval ...
//   - yarn:      yarn promptfoo eval ...
//
// All forms must contain the literal `promptfoo` (or `promptfoo@<ver>`)
// token followed by `eval`. Other subcommands (`promptfoo view`,
// `promptfoo init`) are rejected.
type Adapter struct{}

func (a *Adapter) Matches(cmd []string) bool {
	if len(cmd) == 0 {
		return false
	}
	for i, tok := range cmd {
		base := tok
		if at := strings.Index(tok, "@"); at > 0 {
			base = tok[:at]
		}
		if base != "promptfoo" {
			continue
		}
		// Need an `eval` subcommand somewhere after this token.
		for j := i + 1; j < len(cmd); j++ {
			if cmd[j] == "eval" {
				return true
			}
		}
		return false
	}
	return false
}

// userOutputPath returns the value of --output / -o / --output=<v> in args,
// or "" if not present.
func userOutputPath(args []string) string {
	for i, a := range args {
		switch {
		case a == "--output" || a == "-o":
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, "--output="):
			return strings.TrimPrefix(a, "--output=")
		}
	}
	return ""
}

// buildArgs strips the executable token from cmd[0] and returns the args
// to pass to it, with --output <path> appended unless the user already
// supplied an output flag. The first token of the returned slice is the
// `eval` subcommand (or whatever the user wrote there); cmd[0] itself is
// dropped because the caller invokes exec.Command(cmd[0], buildArgs(...)).
func buildArgs(cmd []string, jsonPath string) []string {
	rest := append([]string{}, cmd[1:]...)
	if userOutputPath(rest) != "" {
		return rest
	}
	return append(rest, "--output", jsonPath)
}
```

- [ ] **Step 4: Run the matcher and arg-injection tests**

Run: `go test ./internal/eval/promptfoo/... -v -run "TestMatches|TestBuildArgs|TestUserOutputPath"`
Expected: every test PASSES.

- [ ] **Step 5: Commit**

```bash
git add internal/eval/promptfoo/adapter.go internal/eval/promptfoo/adapter_test.go
git commit -m "feat(promptfoo): adapter Matches + arg injection for --output"
```

---

## Task 7: Promptfoo adapter — Run flow

**Files:**
- Modify: `internal/eval/promptfoo/adapter.go` (add `Run` method)
- Modify: `internal/eval/promptfoo/adapter_test.go` (add `Run` test with stub child)

`Run` orchestrates: pick a tempfile, build args, invoke the child, parse the resulting file, return `(testResults, metrics, exitCode)`. Tests use a stub `cmd` that writes a known fixture to the requested path.

- [ ] **Step 1: Write a failing Run test**

Append to `internal/eval/promptfoo/adapter_test.go` (the imports from
Task 6 are reused; remove the `var _ = ...` silencers added there as
they're no longer needed):

```go
// fakeChildScript writes a small shell script that copies a fixture to the
// path given after `--output`. Used to stub out `promptfoo eval` in Run tests.
func fakeChildScript(t *testing.T, fixture string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-child shell script is bash-only")
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-promptfoo")
	fixtureSrc := filepath.Join("testdata", fixture)
	body := `#!/usr/bin/env bash
set -e
out=""
for ((i=1;i<=$#;i++)); do
    a="${!i}"
    if [[ "$a" == "--output" || "$a" == "-o" ]]; then
        n=$((i+1))
        out="${!n}"
        break
    elif [[ "$a" == --output=* ]]; then
        out="${a#*=}"
        break
    fi
done
if [[ -z "$out" ]]; then
    echo "fake-promptfoo: no --output flag" >&2
    exit 2
fi
cp "` + fixtureSrc + `" "$out"
`
	if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake-promptfoo: %v", err)
	}
	abs, err := filepath.Abs(scriptPath)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

func TestRunHappyPath(t *testing.T) {
	bin := fakeChildScript(t, "single_assertion.json")
	a := &Adapter{}
	tests, metrics, code := a.Run([]string{bin, "eval", "-c", "x.yaml"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if len(tests) != 1 || !tests[0].Passed {
		t.Fatalf("expected 1 passing test, got %v", tests)
	}
	if len(metrics) != 1 || metrics[0].Name != "eval.contains" {
		t.Fatalf("expected eval.contains metric, got %v", metrics)
	}
}

func TestRunFailingChildPropagatesExit(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-fail")
	body := `#!/usr/bin/env bash
exit 7
`
	if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake-fail: %v", err)
	}
	abs, _ := filepath.Abs(scriptPath)
	a := &Adapter{}
	_, _, code := a.Run([]string{abs, "eval"})
	if code != 7 {
		t.Fatalf("expected exit 7, got %d", code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/eval/promptfoo/... -run TestRun`
Expected: FAIL — `Adapter.Run` does not exist.

- [ ] **Step 3: Implement `Run` in `internal/eval/promptfoo/adapter.go`**

Merge the imports below into the existing import block at the top of
`internal/eval/promptfoo/adapter.go` (which currently only imports
`"strings"` from Task 6 step 3). The new import block becomes:

```go
import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"

	"github.com/bjk95/defrost/internal/models"
	"github.com/bjk95/defrost/internal/runner"
)
```

Then append the `Run` method to the end of the file:

```go
func (a *Adapter) Run(cmd []string) ([]models.TestResult, []*metricspb.Metric, int) {
	if len(cmd) == 0 {
		return nil, nil, 2
	}
	userPath := userOutputPath(cmd[1:])
	tempPath := userPath
	if tempPath == "" {
		f, err := os.CreateTemp("", "defrost-promptfoo-*.json")
		if err != nil {
			fmt.Fprintln(os.Stderr, "defrost:", err)
			return nil, nil, 1
		}
		tempPath = f.Name()
		f.Close()
		defer os.Remove(tempPath)
	}

	args := buildArgs(cmd, tempPath)

	child := exec.Command(cmd[0], args...)
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr

	runErr := child.Run()
	exitCode := 0
	switch e := runErr.(type) {
	case nil:
		// success
	case *exec.ExitError:
		exitCode = e.ExitCode()
	default:
		fmt.Fprintln(os.Stderr, "defrost:", runErr)
		return nil, nil, 1
	}

	f, err := os.Open(tempPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "defrost: promptfoo output not found at", tempPath, ":", err)
		return nil, nil, exitCode
	}
	defer f.Close()

	tests, metrics, parseErr := Parse(f)
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, "defrost:", parseErr)
		return nil, nil, exitCode
	}
	runner.ApplyRepoPrefix(tests)

	return tests, metrics, exitCode
}
```

(The `runner.ApplyRepoPrefix` call mirrors the existing jest adapter at `internal/javascript/jest/adapter.go:240`. It prefixes test IDs with the repo root path so cross-runner test IDs are stable. The existing helper lives at `internal/runner/prefix.go` and is already on `main`.)

- [ ] **Step 4: Run the Run tests**

Run: `go test ./internal/eval/promptfoo/... -run TestRun -v`
Expected: PASS.

- [ ] **Step 5: Run all promptfoo tests**

Run: `go test ./internal/eval/promptfoo/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/eval/promptfoo/adapter.go internal/eval/promptfoo/adapter_test.go
git commit -m "feat(promptfoo): Run orchestrates tempfile, child exec, parse"
```

---

## Task 8: Register the Promptfoo adapter

**Files:**
- Modify: `exec.go`

The registry has new adapter slots in `HandleExecution`. Promptfoo joins.

- [ ] **Step 1: Add the import and registration in `exec.go`**

At the top of `exec.go`, add the import (alphabetically with the existing imports):

```go
"github.com/bjk95/defrost/internal/eval/promptfoo"
```

In `HandleExecution` (line 39 area), find the existing block:

```go
reg := runner.NewRegistry()
reg.Register(golang.Adapter{})
reg.Register(pytest.Adapter{})
reg.Register(&jest.Adapter{})
```

Add the line:

```go
reg.Register(&promptfoo.Adapter{})
```

(Promptfoo's `Adapter` is a pointer receiver since it follows the jest pattern of pointer-receiver methods for consistency with future state.)

- [ ] **Step 2: Run the full Go suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 3: Smoke test the binary recognises the command**

Run: `go run . exec promptfoo`
Expected: exit non-zero with `exec: unsupported test command: "promptfoo"` if `eval` isn't supplied (Matches needs `eval` after `promptfoo`).

Run: `go run . exec promptfoo eval --help` (in a directory without a real `promptfoo` binary on PATH — the command will fail at child exec time, but defrost should select the adapter rather than rejecting the cmd).
Expected: defrost selects the adapter, runs the child, child fails because `promptfoo` isn't installed; defrost exits with the child's error code.

If the local environment has `promptfoo` installed, the second smoke is more meaningful. If not, the failure mode is "child exec failed" rather than "no adapter matched", which is what we want.

- [ ] **Step 4: Commit**

```bash
git add exec.go
git commit -m "feat(exec): register promptfoo adapter"
```

---

## Task 9: Examples + integration smoke test

**Files:**
- Create: `examples/promptfoo/promptfooconfig.yaml`
- Create: `examples/promptfoo/.gitignore`
- Create: `examples/promptfoo/README.md`
- Modify: `.github/workflows/integration.yml`

A tiny eval that lets CI exercise the full pipeline against a real `promptfoo eval` invocation. Two assertions on one prompt — small enough to run in seconds, big enough to verify the metric files are correctly named per assertion type.

- [ ] **Step 1: Create `examples/promptfoo/promptfooconfig.yaml`**

```yaml
description: Defrost smoke test for the promptfoo adapter

prompts:
  - 'What is the capital of {{country}}? Answer in one word.'

providers:
  - id: openai:gpt-4o-mini
    label: openai:gpt-4o-mini

tests:
  - vars:
      country: France
    assert:
      - type: contains
        value: Paris
      - type: llm-rubric
        value: A single-word answer
  - vars:
      country: Germany
    assert:
      - type: contains
        value: Berlin
      - type: llm-rubric
        value: A single-word answer
```

- [ ] **Step 2: Create `examples/promptfoo/.gitignore`**

```
node_modules/
output.json
.defrost-dev/
```

- [ ] **Step 3: Create `examples/promptfoo/README.md`**

```markdown
# promptfoo example

Tiny defrost-instrumented promptfoo eval used by the integration suite.

```sh
# from this directory
npx promptfoo@latest eval -c promptfooconfig.yaml   # bare promptfoo
defrost exec npx promptfoo@latest eval -c promptfooconfig.yaml  # via defrost
```

Requires `OPENAI_API_KEY` in the environment. CI uses the same key from a
gated secret; on forks the job is skipped.
```

- [ ] **Step 4: Read the existing integration workflow**

Run: `cat .github/workflows/integration.yml | head -100`

Identify the existing job structure (jobs are listed for `golang`, `python`, `javascript`, `typescript`). Mirror that shape for promptfoo.

- [ ] **Step 5: Add a promptfoo job**

Append a new `promptfoo:` job to `.github/workflows/integration.yml` matching the structure of the existing `javascript:` job. Sketch (adapt to the workflow's conventions in your branch — variable names, action versions, etc.):

```yaml
  promptfoo:
    needs: unit-tests
    runs-on: ubuntu-latest
    if: ${{ secrets.OPENAI_API_KEY != '' }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.24' }
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - name: Build defrost
        run: go build -o ./defrost .
      - name: Install promptfoo
        run: npm install -g promptfoo@latest
      - name: Run defrost-wrapped eval
        working-directory: examples/promptfoo
        run: ../../defrost exec promptfoo eval -c promptfooconfig.yaml --no-cache --no-persist
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
      - name: Assert results summary
        working-directory: examples/promptfoo
        run: ../../defrost exec promptfoo eval -c promptfooconfig.yaml --no-cache 2>&1 | grep -q 'defrost: results:'
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
```

The `if: ${{ secrets.OPENAI_API_KEY != '' }}` gate skips on forks where the secret isn't available.

- [ ] **Step 6: Run the local Go suite once more**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add examples/promptfoo/ .github/workflows/integration.yml
git commit -m "ci(promptfoo): add example eval and integration job"
```

---

## Self-review

After Task 9, scan back through the plan against the spec sections:

| Spec section | Plan coverage |
|---|---|
| Two-role split (Role A runner / Role B plugin) | Tasks 1-3 (signature change + plumbing), Tasks 5-7 (Promptfoo as Role A runner). Plugin role is a future task — explicitly out of this plan. |
| File-parse vs OTLP receiver | Task 7 implements the file-parse path; OTLP receiver path is unchanged on the merged-from-main branch. |
| Auto-injection of output flag | Task 6 (`buildArgs`, `userOutputPath`); Task 7 (`Run` calls them). |
| Standards mapping (gen_ai.evaluation.* + defrost.eval.*) | Task 5 (`mapComponentResult`) attaches all required attributes via `models.StringAttr` / `models.DoubleAttr`. |
| Adapter-emitted metrics use `*metricspb.Metric` proto, single data point per metric | Task 5 builds gauges with one `NumberDataPoint` each. Task 1 makes the signature carry them. |
| Promptfoo's verified `--output` / `-o` flag | Task 6 tests cover both forms plus `--output=value`. |
| Recognises npx / pnpm / yarn forms | Task 6 `TestMatches` covers them. |
| Pass/fail dual write (TestResult + Metric) | Task 5 `Parse` returns both slices; Task 7 `Run` returns both. |
| Persist failure preserves exit code | Inherited from existing exec.go logic — Task 3 plumbs metrics without disturbing it. |

Verify nothing in the plan references `MetricEntry` (the original-spec Go domain type that was replaced by `*metricspb.Metric` proto), `EvalRecord`, `FindAll`, `PluginAdapter`, `eval.PluginAdapter` — those are spec concepts for the DeepEval/RAGAS plan (step 2), not this one.
