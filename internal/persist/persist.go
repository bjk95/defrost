package persist

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	cmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	ctracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/bjk95/defrost/internal/models"
)

const (
	DefaultDataBranch = "_defrost"

	botName  = "defrost[bot]"
	botEmail = "defrost[bot]@users.noreply.github.com"

	readme = "# defrost data branch\n\nManaged by the defrost CLI. Do not edit by hand.\n"

	maxPushAttempts = 5

	rootSpanName = "defrost.run"

	// fileSuffix is appended to every per-trace and per-run-metrics file
	// on disk. Zstd-compressed OTLP/Protobuf.
	fileSuffix = ".otlp.pb.zst"
)

// RootSpanName is the OTel span.Name used for every defrost.run root span.
// Exported so the serve layer can find root spans by name without
// hard-coding the literal.
const RootSpanName = rootSpanName

// Options controls Backend creation.
type Options struct {
	RepoDir    string
	DataBranch string // "" → DefaultDataBranch
	AuthToken  string
	Dev        bool
}

const DevDir = ".defrost-dev"

// Backend is the swappable persistence layer. Spans and metrics are
// passed in as canonical OTel proto types and stored as zstd-compressed
// OTLP/Protobuf payloads — one file per signal per defrost exec
// invocation, named by the run's trace_id and partitioned by run-start
// UTC date.
type Backend interface {
	InitialisePersistence() error

	// InsertNewRun atomically persists everything produced by one
	// defrost exec invocation: the root run span, every test span, and
	// every metric data point. Writes one trace file and (if metrics
	// are present) one metrics file. Span and metric Resources may
	// differ — callers strip run-unique fields from the metric Resource
	// to keep metric cardinality bounded.
	InsertNewRun(traces []*tracepb.ResourceSpans, metrics []*metricspb.ResourceMetrics) error

	// GetTestHistory returns every span whose Name == testName across
	// every persisted trace file, sorted oldest first by start time.
	// Each returned element is a ResourceSpans containing exactly one
	// Span. Empty slice when nothing matches.
	GetTestHistory(testName string) ([]*tracepb.ResourceSpans, error)

	// GetSuppressions returns the current list of suppressed test IDs,
	// or an empty slice if none have been recorded. A missing data branch
	// is not an error.
	GetSuppressions() ([]string, error)
	// UpdateSuppressions reads the current list, applies mutate, and
	// writes the result. msg is the commit message used for the update.
	UpdateSuppressions(mutate func([]string) []string, msg string) error

	// LoadAll returns every root run span and every test span across
	// all persisted trace files, grouped by encoded span name. Used by
	// `defrost serve` to populate the heatmap grid in a single read
	// instead of N. Returns nil slices/maps when there is no data yet.
	LoadAll() (rootSpans []*tracepb.ResourceSpans, byEncodedName map[string][]*tracepb.ResourceSpans, err error)

	// LoadAllMetrics returns every persisted metric data point across
	// all metrics files. Each element is a ResourceMetrics containing
	// exactly one Metric — the same shape callers produced before the
	// per-run bundling under WrapMetricsInResource. Returns nil when
	// there is no metrics data on disk.
	LoadAllMetrics() ([]*metricspb.ResourceMetrics, error)

	// LoadAllWithProgress is LoadAll with phase boundaries emitted to
	// progress (clone, spans, parse). Used by the serve boot screen to
	// stream a real terminal log as the data branch is cloned and
	// parsed. Pass nil to opt out — equivalent to LoadAll.
	LoadAllWithProgress(progress ProgressFn) (rootSpans []*tracepb.ResourceSpans, byEncodedName map[string][]*tracepb.ResourceSpans, err error)

	// LoadAllMetricsWithProgress is LoadAllMetrics with progress events
	// emitted (metrics phase + final data point count). Pass nil to opt
	// out — equivalent to LoadAllMetrics.
	LoadAllMetricsWithProgress(progress ProgressFn) ([]*metricspb.ResourceMetrics, error)

	// DropHistory inventories the data branch and (after confirm returns
	// true) rewrites it via a single orphan commit force-pushed with
	// --force-with-lease. See drop.go for full semantics.
	DropHistory(sel DropSelector, confirm func(DropPlan) bool) error
}

// ProgressFn is the callback shape used to report per-phase progress
// during a read. phase is one of: clone, spans, parse, metrics. detail
// is the active sub-step (e.g. the actual git command); stream is an
// optional log line carrying real numbers (e.g. "found 50 run files").
// Either detail or stream may be empty.
type ProgressFn func(phase, detail, stream string)

// New returns the Backend implied by opts. Dev mode selects the local
// scratch backend; otherwise the git-data-branch backend is used.
func New(opts Options) Backend {
	if opts.Dev {
		return &fileBackend{dir: filepath.Join(opts.RepoDir, DevDir)}
	}
	return &gitBackend{opts: opts}
}

// ErrNoOrigin is returned when the user's repo has no origin remote configured.
var ErrNoOrigin = errors.New("no origin remote configured")

// EncodeName escapes a span or metric name into a filesystem-safe segment.
// Reversible via DecodeName.
func EncodeName(name string) string { return url.PathEscape(name) }

// DecodeName is the inverse of EncodeName.
func DecodeName(id string) (string, error) { return url.PathUnescape(id) }

// NewRunID returns a sortable-by-time, collision-resistant run identifier.
// Format: <16 hex of UnixNano>-<8 hex of crypto/rand>.
func NewRunID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return fmt.Sprintf("%016x-%s", time.Now().UnixNano(), hex.EncodeToString(buf[:]))
}

// DetectRunContext builds a RunContext for one defrost exec invocation by
// inspecting the user's repo, environment variables, and Go runtime.
// Returned Resource attributes follow OTel CI/CD and VCS semantic
// conventions where applicable; defrost-private keys use a `defrost.*`
// prefix.
func DetectRunContext(opts Options, cmd []string, defrostVersion string) (models.RunContext, error) {
	if _, err := runGit(opts.RepoDir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return models.RunContext{}, fmt.Errorf("not a git repo at %s: %w", opts.RepoDir, err)
	}

	runID := NewRunID()
	attrs := []*commonpb.KeyValue{
		models.StringAttr("service.name", "defrost"),
		models.StringAttr("service.version", defrostVersion),
		models.StringAttr("cicd.pipeline.run.id", runID),
		models.StringAttr("host.os.type", goruntime.GOOS),
		models.StringAttr("host.arch", goruntime.GOARCH),
		models.StringAttr("process.runtime.version", goruntime.Version()),
		models.StringArrayAttr("defrost.cmd", cmd),
		models.StringAttr("defrost.cmd_hash", cmdHash(cmd)),
		models.StringAttr("defrost.runner", inferRunner(cmd)),
	}

	if out, err := runGit(opts.RepoDir, "log", "-1", "--format=%H%n%P%n%ae%n%an"); err == nil {
		lines := strings.SplitN(out, "\n", 4)
		if len(lines) >= 1 && lines[0] != "" {
			attrs = append(attrs, models.StringAttr("vcs.repository.ref.revision", lines[0]))
		}
		if len(lines) >= 2 {
			parents := strings.Fields(lines[1])
			if len(parents) > 0 {
				attrs = append(attrs, models.StringAttr("defrost.parent_commit", parents[0]))
			}
		}
		if len(lines) >= 3 && lines[2] != "" {
			attrs = append(attrs, models.StringAttr("defrost.author_email", lines[2]))
		}
		if len(lines) >= 4 && lines[3] != "" {
			attrs = append(attrs, models.StringAttr("defrost.author_name", lines[3]))
		}
	}

	if out, err := runGit(opts.RepoDir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && out != "HEAD" && out != "" {
		attrs = append(attrs, models.StringAttr("vcs.repository.ref.name", out))
	} else if v := os.Getenv("GITHUB_HEAD_REF"); v != "" {
		attrs = append(attrs, models.StringAttr("vcs.repository.ref.name", v))
	} else if v := os.Getenv("GITHUB_REF_NAME"); v != "" {
		attrs = append(attrs, models.StringAttr("vcs.repository.ref.name", v))
	}

	if pr := parsePRFromEnv(); pr != 0 {
		attrs = append(attrs, models.StringAttr("vcs.repository.change.id", strconv.Itoa(pr)))
	}

	dirty, dirtyHash := workingTreeStatus(opts.RepoDir)
	attrs = append(attrs, models.BoolAttr("defrost.dirty", dirty))
	if dirtyHash != "" {
		attrs = append(attrs, models.StringAttr("defrost.dirty_hash", dirtyHash))
	}

	now := time.Now().UnixNano()
	return models.RunContext{
		RunID:             runID,
		TraceID:           models.DeriveTraceID(runID),
		RootSpanID:        models.NewSpanID(),
		Resource:          &resourcepb.Resource{Attributes: attrs},
		StartTimeUnixNano: now,
	}, nil
}

// MetricResource returns the subset of the run's Resource attributes that
// is safe to attach to metrics — i.e. excluding fields that would
// explode time-series cardinality (run_id, full cmd, dirty hash, author,
// commit). Spans keep the full identity Resource because spans aren't
// aggregated; metrics do not.
func MetricResource(run models.RunContext) *resourcepb.Resource {
	if run.Resource == nil {
		return nil
	}
	skip := map[string]struct{}{
		"cicd.pipeline.run.id":        {},
		"defrost.cmd":                 {},
		"defrost.dirty_hash":          {},
		"defrost.author_email":        {},
		"defrost.author_name":         {},
		"defrost.parent_commit":       {},
		"vcs.repository.ref.revision": {},
	}
	out := &resourcepb.Resource{}
	for _, kv := range run.Resource.Attributes {
		if _, drop := skip[kv.Key]; drop {
			continue
		}
		out.Attributes = append(out.Attributes, kv)
	}
	return out
}

// inferRunner returns a stable token identifying the runner used in cmd:
// "go", "pytest", "jest", or "" when nothing matches.
func inferRunner(cmd []string) string {
	if len(cmd) == 0 {
		return ""
	}
	switch {
	case len(cmd) >= 2 && cmd[0] == "go" && cmd[1] == "test":
		return "go"
	case cmd[0] == "pytest" || cmd[0] == "py.test":
		return "pytest"
	case cmd[0] == "jest" || cmd[0] == "npx" && len(cmd) > 1 && cmd[1] == "jest" || cmd[0] == "npm" && len(cmd) > 1 && cmd[1] == "test":
		return "jest"
	}
	return ""
}

// NewRootSpan returns the bookkeeping span representing one defrost exec
// invocation. End time and status are filled in by the caller after the
// child exits and persistence either succeeds or fails.
func NewRootSpan(run models.RunContext) *tracepb.Span {
	return &tracepb.Span{
		TraceId:           run.TraceID,
		SpanId:            run.RootSpanID,
		Name:              rootSpanName,
		Kind:              tracepb.Span_SPAN_KIND_INTERNAL,
		StartTimeUnixNano: uint64(run.StartTimeUnixNano),
		Status:            &tracepb.Status{Code: tracepb.Status_STATUS_CODE_UNSET},
	}
}

// RunDurationMetric returns the auto-emitted gauge that records the
// wall-clock duration of one defrost exec invocation, in milliseconds.
// The metric name embeds the run's path-from-repo-root and the wrapped
// command so each invocation site has its own time series.
func RunDurationMetric(run models.RunContext, cmd []string, repoDir string, end time.Time) *metricspb.Metric {
	durationMs := float64(end.UnixNano()-run.StartTimeUnixNano) / 1e6
	return &metricspb.Metric{
		Name:        "defrost.run." + runFQN(repoDir, cmd),
		Description: "Wall-clock duration of one defrost exec invocation.",
		Unit:        "ms",
		Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
			DataPoints: []*metricspb.NumberDataPoint{{
				StartTimeUnixNano: uint64(run.StartTimeUnixNano),
				TimeUnixNano:      uint64(end.UnixNano()),
				Value:             &metricspb.NumberDataPoint_AsDouble{AsDouble: durationMs},
			}},
		}},
	}
}

// runFQN builds the fully qualified run identifier embedded in the
// duration metric name: "<path-from-repo-root>¬<space-joined cmd>" when
// invoked from a subdirectory, or just the command when invoked at the
// repo root (or when the prefix lookup fails).
func runFQN(repoDir string, cmd []string) string {
	joined := strings.Join(cmd, " ")
	prefix, err := runGit(repoDir, "rev-parse", "--show-prefix")
	if err != nil || prefix == "" {
		return joined
	}
	return strings.TrimSuffix(prefix, "/") + "¬" + joined
}

// WrapSpansInResource bundles every span produced by one defrost exec
// invocation (root + every test span) into a single *tracepb.ResourceSpans
// — the on-disk shape under traces/. The caller passes the run's full
// Resource; we stash the spans under a single ScopeSpans named "defrost"
// so all attribute identity lives exactly once on the Resource.
//
// Returned slice has at most one element; nil if no spans.
func WrapSpansInResource(resource *resourcepb.Resource, spans []*tracepb.Span) []*tracepb.ResourceSpans {
	if len(spans) == 0 {
		return nil
	}
	return []*tracepb.ResourceSpans{{
		Resource: resource,
		ScopeSpans: []*tracepb.ScopeSpans{{
			Scope: &commonpb.InstrumentationScope{Name: "defrost"},
			Spans: spans,
		}},
	}}
}

// WrapMetricsInResource bundles every metric data point emitted during
// one defrost exec invocation into a single *metricspb.ResourceMetrics.
//
// Returned slice has at most one element; nil if no metrics.
func WrapMetricsInResource(resource *resourcepb.Resource, metrics []*metricspb.Metric) []*metricspb.ResourceMetrics {
	if len(metrics) == 0 {
		return nil
	}
	return []*metricspb.ResourceMetrics{{
		Resource: resource,
		ScopeMetrics: []*metricspb.ScopeMetrics{{
			Scope:   &commonpb.InstrumentationScope{Name: "defrost"},
			Metrics: metrics,
		}},
	}}
}

// SpanFromResourceSpans returns the first *tracepb.Span inside a
// ResourceSpans we wrote. Returns nil if there are no spans.
//
// Used by the read path (history, dashboard) where spans are filtered
// individually after a file is decoded — at decode time the per-trace
// file has been split back into one ResourceSpans per span via
// SplitResourceSpans.
func SpanFromResourceSpans(rs *tracepb.ResourceSpans) *tracepb.Span {
	if rs == nil || len(rs.ScopeSpans) == 0 || len(rs.ScopeSpans[0].Spans) == 0 {
		return nil
	}
	return rs.ScopeSpans[0].Spans[0]
}

// MetricFromResourceMetrics returns the first *metricspb.Metric inside
// a ResourceMetrics we wrote.
func MetricFromResourceMetrics(rm *metricspb.ResourceMetrics) *metricspb.Metric {
	if rm == nil || len(rm.ScopeMetrics) == 0 || len(rm.ScopeMetrics[0].Metrics) == 0 {
		return nil
	}
	return rm.ScopeMetrics[0].Metrics[0]
}

// SplitResourceSpans expands one *tracepb.ResourceSpans (containing N
// spans across its ScopeSpans) into N single-span *tracepb.ResourceSpans
// values — each carrying the original Resource. This is the inverse of
// WrapSpansInResource at read time, so existing callers that expect
// one Span per ResourceSpans (history, dashboard grid, run resolver)
// keep working unchanged.
func SplitResourceSpans(rs *tracepb.ResourceSpans) []*tracepb.ResourceSpans {
	if rs == nil {
		return nil
	}
	var out []*tracepb.ResourceSpans
	for _, ss := range rs.ScopeSpans {
		for _, s := range ss.Spans {
			out = append(out, &tracepb.ResourceSpans{
				Resource:   rs.Resource,
				ScopeSpans: []*tracepb.ScopeSpans{{Scope: ss.Scope, Spans: []*tracepb.Span{s}}},
			})
		}
	}
	return out
}

// SplitResourceMetrics is the inverse of WrapMetricsInResource at read
// time: it expands one *metricspb.ResourceMetrics into one
// *metricspb.ResourceMetrics per *metricspb.Metric, each carrying the
// original Resource. Existing callers expect one Metric per record.
func SplitResourceMetrics(rm *metricspb.ResourceMetrics) []*metricspb.ResourceMetrics {
	if rm == nil {
		return nil
	}
	var out []*metricspb.ResourceMetrics
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out = append(out, &metricspb.ResourceMetrics{
				Resource:     rm.Resource,
				ScopeMetrics: []*metricspb.ScopeMetrics{{Scope: sm.Scope, Metrics: []*metricspb.Metric{m}}},
			})
		}
	}
	return out
}

// gitBackend stores spans and metrics on a dedicated git data branch.
type gitBackend struct{ opts Options }

func (b *gitBackend) InitialisePersistence() error { return nil }

func (b *gitBackend) InsertNewRun(traces []*tracepb.ResourceSpans, metrics []*metricspb.ResourceMetrics) error {
	if len(traces) == 0 && len(metrics) == 0 {
		return nil
	}

	branch := b.dataBranch()

	remoteURL, err := resolveTargetURL(b.opts)
	if err != nil {
		return err
	}

	workDir, err := os.MkdirTemp("", "defrost-data-")
	if err != nil {
		return fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(workDir)

	branchExisted, err := openOrInitDataRepo(workDir, remoteURL, branch)
	if err != nil {
		return err
	}

	if !branchExisted {
		if err := writeSeed(workDir); err != nil {
			return err
		}
	}

	if err := writeRunFiles(workDir, traces, metrics); err != nil {
		return err
	}

	if err := commitAll(workDir, commitMessage(traces, metrics)); err != nil {
		return err
	}

	return pushWithRetry(workDir, branch, branchExisted)
}

func (b *gitBackend) GetTestHistory(testName string) ([]*tracepb.ResourceSpans, error) {
	dir, cleanup, err := b.cloneForRead()
	if err != nil || dir == "" {
		return nil, err
	}
	defer cleanup()
	return readTestHistoryFromDir(dir, testName)
}

func (b *gitBackend) LoadAll() ([]*tracepb.ResourceSpans, map[string][]*tracepb.ResourceSpans, error) {
	return b.LoadAllWithProgress(nil)
}

func (b *gitBackend) LoadAllMetrics() ([]*metricspb.ResourceMetrics, error) {
	return b.LoadAllMetricsWithProgress(nil)
}

func (b *gitBackend) LoadAllWithProgress(progress ProgressFn) ([]*tracepb.ResourceSpans, map[string][]*tracepb.ResourceSpans, error) {
	emit := nopProgress(progress)
	emit("clone", "git clone --depth=1 --single-branch --branch "+b.dataBranch(), "")
	dir, cleanup, err := b.cloneForRead()
	if err != nil || dir == "" {
		return nil, nil, err
	}
	defer cleanup()
	return readAllSpansWithProgress(dir, emit)
}

func (b *gitBackend) LoadAllMetricsWithProgress(progress ProgressFn) ([]*metricspb.ResourceMetrics, error) {
	emit := nopProgress(progress)
	dir, cleanup, err := b.cloneForRead()
	if err != nil || dir == "" {
		return nil, err
	}
	defer cleanup()
	return readAllMetricsWithProgress(dir, emit)
}

// nopProgress wraps a possibly-nil ProgressFn so callers can emit
// unconditionally without per-call nil checks.
func nopProgress(fn ProgressFn) ProgressFn {
	if fn == nil {
		return func(string, string, string) {}
	}
	return fn
}

func (b *gitBackend) dataBranch() string {
	if b.opts.DataBranch != "" {
		return b.opts.DataBranch
	}
	return DefaultDataBranch
}

// cloneForRead clones the data branch with a strategy tuned for bulk
// reads: full checkout (no blob filtering) so subsequent file opens hit
// the local working tree and don't require per-blob HTTP fetches. Returns
// ("", noop, nil) when the data branch doesn't exist on the remote.
func (b *gitBackend) cloneForRead() (string, func(), error) {
	branch := b.dataBranch()
	remoteURL, err := resolveTargetURL(b.opts)
	if err != nil {
		return "", func() {}, err
	}
	exists, err := branchExistsOnRemote(remoteURL, branch)
	if err != nil {
		return "", func() {}, err
	}
	if !exists {
		return "", func() {}, nil
	}

	workDir, err := os.MkdirTemp("", "defrost-read-")
	if err != nil {
		return "", func() {}, fmt.Errorf("mktemp: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(workDir) }
	_ = os.Remove(workDir) // git clone wants the path absent

	if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("clone data branch: %w", err)
	}
	return workDir, cleanup, nil
}

// fileBackend writes spans/metrics to a plain directory; no git operations.
type fileBackend struct{ dir string }

func (b *fileBackend) InitialisePersistence() error {
	return os.MkdirAll(b.dir, 0o755)
}

func (b *fileBackend) InsertNewRun(traces []*tracepb.ResourceSpans, metrics []*metricspb.ResourceMetrics) error {
	if len(traces) == 0 && len(metrics) == 0 {
		return nil
	}
	if err := b.InitialisePersistence(); err != nil {
		return err
	}
	return writeRunFiles(b.dir, traces, metrics)
}

func (b *fileBackend) GetTestHistory(testName string) ([]*tracepb.ResourceSpans, error) {
	if _, err := os.Stat(b.dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return readTestHistoryFromDir(b.dir, testName)
}

func (b *fileBackend) LoadAll() ([]*tracepb.ResourceSpans, map[string][]*tracepb.ResourceSpans, error) {
	return b.LoadAllWithProgress(nil)
}

func (b *fileBackend) LoadAllMetrics() ([]*metricspb.ResourceMetrics, error) {
	return b.LoadAllMetricsWithProgress(nil)
}

func (b *fileBackend) LoadAllWithProgress(progress ProgressFn) ([]*tracepb.ResourceSpans, map[string][]*tracepb.ResourceSpans, error) {
	if _, err := os.Stat(b.dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	return readAllSpansWithProgress(b.dir, nopProgress(progress))
}

func (b *fileBackend) LoadAllMetricsWithProgress(progress ProgressFn) ([]*metricspb.ResourceMetrics, error) {
	if _, err := os.Stat(b.dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return readAllMetricsWithProgress(b.dir, nopProgress(progress))
}

// --- write helpers ---

// zstdEncoder is shared across writes. Default compression level is fine
// for text-heavy proto payloads with run metadata.
var (
	zstdEncoderOnce sync.Once
	zstdEncoder     *zstd.Encoder
	zstdEncoderErr  error
)

func sharedZstdEncoder() (*zstd.Encoder, error) {
	zstdEncoderOnce.Do(func() {
		zstdEncoder, zstdEncoderErr = zstd.NewWriter(nil)
	})
	return zstdEncoder, zstdEncoderErr
}

var (
	zstdDecoderOnce sync.Once
	zstdDecoder     *zstd.Decoder
	zstdDecoderErr  error
)

func sharedZstdDecoder() (*zstd.Decoder, error) {
	zstdDecoderOnce.Do(func() {
		zstdDecoder, zstdDecoderErr = zstd.NewReader(nil)
	})
	return zstdDecoder, zstdDecoderErr
}

// writeSeed writes the README at branch creation time. We deliberately do
// NOT write a `.gitattributes` with `merge=union` rules anymore: per-trace
// files are writer-owned (one file per run, named by trace_id) so two
// concurrent runs can never target the same path.
func writeSeed(workDir string) error {
	return os.WriteFile(filepath.Join(workDir, "README.md"), []byte(readme), 0o644)
}

// writeRunFiles is the single entry point used by both backends. It
// writes at most one zstd-compressed OTLP/protobuf file under traces/ and
// at most one under metrics/, partitioned by run-start UTC date.
func writeRunFiles(workDir string, traces []*tracepb.ResourceSpans, metrics []*metricspb.ResourceMetrics) error {
	if len(traces) > 0 {
		if err := writeTraceFile(workDir, traces); err != nil {
			return err
		}
	}
	if len(metrics) > 0 {
		if err := writeMetricsFile(workDir, traces, metrics); err != nil {
			return err
		}
	}
	return nil
}

func writeTraceFile(workDir string, traces []*tracepb.ResourceSpans) error {
	traceID, runStart := traceIdentityFromSpans(traces)
	if len(traceID) == 0 {
		return errors.New("write traces: no trace_id in run")
	}
	data, err := marshalTraceRequest(traces)
	if err != nil {
		return fmt.Errorf("marshal traces: %w", err)
	}
	compressed, err := compressZstd(data)
	if err != nil {
		return fmt.Errorf("compress traces: %w", err)
	}
	path := tracePath(workDir, traceID, runStart)
	return writeFileAtomic(path, compressed)
}

func writeMetricsFile(workDir string, traces []*tracepb.ResourceSpans, metrics []*metricspb.ResourceMetrics) error {
	traceID, runStart := traceIdentityFromSpans(traces)
	data, err := marshalMetricsRequest(metrics)
	if err != nil {
		return fmt.Errorf("marshal metrics: %w", err)
	}
	compressed, err := compressZstd(data)
	if err != nil {
		return fmt.Errorf("compress metrics: %w", err)
	}
	path := metricsPath(workDir, traceID, runStart)
	return writeFileAtomic(path, compressed)
}

// traceIdentityFromSpans returns the run's trace_id and start time so the
// caller can derive the date-partitioned filename. Reads from the root
// span's TraceId / StartTimeUnixNano. Falls back to time.Now() if the root
// span has no start time set (shouldn't happen on a real run, but keeps
// the writer robust).
func traceIdentityFromSpans(traces []*tracepb.ResourceSpans) ([]byte, time.Time) {
	for _, rs := range traces {
		for _, ss := range rs.ScopeSpans {
			for _, s := range ss.Spans {
				if s == nil || len(s.TraceId) == 0 {
					continue
				}
				ns := int64(s.StartTimeUnixNano)
				if ns == 0 {
					return s.TraceId, time.Now().UTC()
				}
				return s.TraceId, time.Unix(0, ns).UTC()
			}
		}
	}
	return nil, time.Time{}
}

// marshalTraceRequest builds an ExportTraceServiceRequest carrying every
// ResourceSpans the caller passed in, then proto-marshals it. This is the
// canonical OTLP/Protobuf payload shape.
func marshalTraceRequest(traces []*tracepb.ResourceSpans) ([]byte, error) {
	req := &ctracepb.ExportTraceServiceRequest{ResourceSpans: traces}
	return proto.Marshal(req)
}

// marshalMetricsRequest builds an ExportMetricsServiceRequest the same way
// for metrics.
func marshalMetricsRequest(metrics []*metricspb.ResourceMetrics) ([]byte, error) {
	req := &cmetricspb.ExportMetricsServiceRequest{ResourceMetrics: metrics}
	return proto.Marshal(req)
}

func compressZstd(data []byte) ([]byte, error) {
	enc, err := sharedZstdEncoder()
	if err != nil {
		return nil, err
	}
	return enc.EncodeAll(data, nil), nil
}

func decompressZstd(data []byte) ([]byte, error) {
	dec, err := sharedZstdDecoder()
	if err != nil {
		return nil, err
	}
	return dec.DecodeAll(data, nil)
}

// tracePath returns the date-partitioned absolute path for one run's
// trace file: <workDir>/traces/<YYYY>/<MM>/<DD>/<hex trace_id>.otlp.pb.zst.
func tracePath(workDir string, traceID []byte, runStart time.Time) string {
	return signalPath(workDir, "traces", traceID, runStart)
}

// metricsPath is the metrics counterpart of tracePath.
func metricsPath(workDir string, traceID []byte, runStart time.Time) string {
	return signalPath(workDir, "metrics", traceID, runStart)
}

func signalPath(workDir, signal string, traceID []byte, runStart time.Time) string {
	id := hex.EncodeToString(traceID)
	if runStart.IsZero() {
		runStart = time.Now().UTC()
	}
	y, m, d := runStart.UTC().Date()
	return filepath.Join(workDir, signal,
		fmt.Sprintf("%04d", y),
		fmt.Sprintf("%02d", int(m)),
		fmt.Sprintf("%02d", d),
		id+fileSuffix,
	)
}

// writeFileAtomic writes data to path atomically: write a sibling .tmp,
// fsync the file, rename onto the final name, then fsync the parent dir
// so the rename itself survives a crash. Directory fsync is a no-op on
// macOS APFS / Windows but required on Linux ext4/xfs to make the rename
// durable; calling it everywhere is cheap and harmless.
func writeFileAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// --- read helpers ---

// readTestHistoryFromDir walks every per-run trace file under dir/traces,
// extracts spans whose Name matches testName, and returns them sorted
// oldest-first by start time. Decoding is parallelised across CPU cores.
func readTestHistoryFromDir(dir, testName string) ([]*tracepb.ResourceSpans, error) {
	tracesDir := filepath.Join(dir, "traces")
	if _, err := os.Stat(tracesDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []*tracepb.ResourceSpans
	err := walkOTLPTraceFilesParallel(tracesDir, func(rss []*tracepb.ResourceSpans) {
		for _, rs := range rss {
			for _, ss := range rs.ScopeSpans {
				for _, s := range ss.Spans {
					if s.Name != testName {
						continue
					}
					out = append(out, &tracepb.ResourceSpans{
						Resource:   rs.Resource,
						ScopeSpans: []*tracepb.ScopeSpans{{Scope: ss.Scope, Spans: []*tracepb.Span{s}}},
					})
				}
			}
		}
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		a := SpanFromResourceSpans(out[i])
		b := SpanFromResourceSpans(out[j])
		if a == nil || b == nil {
			return false
		}
		return a.StartTimeUnixNano < b.StartTimeUnixNano
	})
	return out, nil
}

// readAllSpansWithProgress walks every per-run trace file, splits each
// into one ResourceSpans per span, separates root spans from test spans,
// and groups test spans by encoded span name (matching the existing
// serve-layer key shape). Decoding is parallelised across CPU cores.
// Emits one "spans" event with the file count, then one "parse" event
// per file decoded. progress must be non-nil — call sites use
// nopProgress to wrap.
func readAllSpansWithProgress(dir string, progress ProgressFn) ([]*tracepb.ResourceSpans, map[string][]*tracepb.ResourceSpans, error) {
	tracesDir := filepath.Join(dir, "traces")
	if _, err := os.Stat(tracesDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	files, err := listOTLPFiles(tracesDir)
	if err != nil {
		return nil, nil, err
	}
	progress("spans", "scanning runs/*.otlp.pb.zst", fmt.Sprintf("found %d run files", len(files)))
	var roots []*tracepb.ResourceSpans
	byEncodedName := make(map[string][]*tracepb.ResourceSpans)
	progress("parse", "decoding OTLP ResourceSpans", "")
	parsed := 0
	emitEvery := 1
	if len(files) > 10 {
		emitEvery = len(files) / 10
	}
	err = walkOTLPTraceFilesParallelFiles(files, func(rss []*tracepb.ResourceSpans) {
		for _, rs := range rss {
			for _, ss := range rs.ScopeSpans {
				for _, s := range ss.Spans {
					single := &tracepb.ResourceSpans{
						Resource:   rs.Resource,
						ScopeSpans: []*tracepb.ScopeSpans{{Scope: ss.Scope, Spans: []*tracepb.Span{s}}},
					}
					if s.Name == rootSpanName {
						roots = append(roots, single)
						continue
					}
					byEncodedName[EncodeName(s.Name)] = append(byEncodedName[EncodeName(s.Name)], single)
				}
			}
		}
		parsed++
		if parsed%emitEvery == 0 || parsed == len(files) {
			progress("parse", "", fmt.Sprintf("parsed %d/%d runs", parsed, len(files)))
		}
	})
	if err != nil {
		return nil, nil, err
	}
	return roots, byEncodedName, nil
}

// readAllMetricsWithProgress walks every per-run metrics file and
// returns one *metricspb.ResourceMetrics per stored Metric (matching
// the existing caller contract from the NDJSON era). Decoding is
// parallelised across CPU cores. Emits one "metrics" event before the
// walk and a final stream event reporting the parsed data point count.
// progress must be non-nil — call sites use nopProgress to wrap.
func readAllMetricsWithProgress(dir string, progress ProgressFn) ([]*metricspb.ResourceMetrics, error) {
	metricsDir := filepath.Join(dir, "metrics")
	if _, err := os.Stat(metricsDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	progress("metrics", "decoding OTLP ResourceMetrics", "")
	var out []*metricspb.ResourceMetrics
	err := walkOTLPMetricsFilesParallel(metricsDir, func(rms []*metricspb.ResourceMetrics) {
		for _, rm := range rms {
			out = append(out, SplitResourceMetrics(rm)...)
		}
	})
	if err != nil {
		return nil, err
	}
	progress("metrics", "", fmt.Sprintf("parsed %d metric data points", len(out)))
	return out, nil
}

// listOTLPFiles returns every <root>/<YYYY>/<MM>/<DD>/<id>.otlp.pb.zst
// path beneath root, in arbitrary order. Returns an empty slice if root
// does not exist.
func listOTLPFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), fileSuffix) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// walkOTLPTraceFilesParallel decodes every trace file under root with a
// worker pool sized to NumCPU, calling emit on the main goroutine for each
// file's worth of decoded ResourceSpans. emit is serialised across
// workers so the caller can mutate shared maps/slices without locks.
func walkOTLPTraceFilesParallel(root string, emit func([]*tracepb.ResourceSpans)) error {
	files, err := listOTLPFiles(root)
	if err != nil {
		return err
	}
	return walkOTLPTraceFilesParallelFiles(files, emit)
}

// walkOTLPTraceFilesParallelFiles is walkOTLPTraceFilesParallel with the
// file list pre-resolved by the caller. Used so progress reporters can
// take a count *before* the parallel decode starts.
func walkOTLPTraceFilesParallelFiles(files []string, emit func([]*tracepb.ResourceSpans)) error {
	if len(files) == 0 {
		return nil
	}
	type result struct {
		path string
		rss  []*tracepb.ResourceSpans
		err  error
	}
	jobs := make(chan string)
	results := make(chan result)
	workers := goruntime.NumCPU()
	if workers > len(files) {
		workers = len(files)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				req, err := readOTLPTraceFile(p)
				if err != nil {
					results <- result{path: p, err: err}
					continue
				}
				results <- result{path: p, rss: req.GetResourceSpans()}
			}
		}()
	}
	go func() {
		for _, p := range files {
			jobs <- p
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	for r := range results {
		if r.err != nil {
			return fmt.Errorf("decode %s: %w", r.path, r.err)
		}
		emit(r.rss)
	}
	return nil
}

func walkOTLPMetricsFilesParallel(root string, emit func([]*metricspb.ResourceMetrics)) error {
	files, err := listOTLPFiles(root)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	type result struct {
		path string
		rms  []*metricspb.ResourceMetrics
		err  error
	}
	jobs := make(chan string)
	results := make(chan result)
	workers := goruntime.NumCPU()
	if workers > len(files) {
		workers = len(files)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				req, err := readOTLPMetricsFile(p)
				if err != nil {
					results <- result{path: p, err: err}
					continue
				}
				results <- result{path: p, rms: req.GetResourceMetrics()}
			}
		}()
	}
	go func() {
		for _, p := range files {
			jobs <- p
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	for r := range results {
		if r.err != nil {
			return fmt.Errorf("decode %s: %w", r.path, r.err)
		}
		emit(r.rms)
	}
	return nil
}

func readOTLPTraceFile(path string) (*ctracepb.ExportTraceServiceRequest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	plain, err := decompressZstd(raw)
	if err != nil {
		return nil, fmt.Errorf("zstd decode: %w", err)
	}
	req := &ctracepb.ExportTraceServiceRequest{}
	if err := proto.Unmarshal(plain, req); err != nil {
		return nil, fmt.Errorf("proto unmarshal: %w", err)
	}
	return req, nil
}

func readOTLPMetricsFile(path string) (*cmetricspb.ExportMetricsServiceRequest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	plain, err := decompressZstd(raw)
	if err != nil {
		return nil, fmt.Errorf("zstd decode: %w", err)
	}
	req := &cmetricspb.ExportMetricsServiceRequest{}
	if err := proto.Unmarshal(plain, req); err != nil {
		return nil, fmt.Errorf("proto unmarshal: %w", err)
	}
	return req, nil
}

func commitMessage(traces []*tracepb.ResourceSpans, metrics []*metricspb.ResourceMetrics) string {
	short := ""
	spanCount := 0
	for _, rs := range traces {
		for _, ss := range rs.ScopeSpans {
			spanCount += len(ss.Spans)
		}
		if short != "" {
			continue
		}
		if commit := models.ResourceString(rs.Resource, "vcs.repository.ref.revision"); commit != "" {
			short = commit
			if len(short) > 7 {
				short = short[:7]
			}
			continue
		}
		if runID := models.ResourceString(rs.Resource, "cicd.pipeline.run.id"); runID != "" {
			short = runID
			if len(short) > 8 {
				short = short[:8]
			}
		}
	}
	if short == "" {
		short = "unknown"
	}
	metricCount := 0
	for _, rm := range metrics {
		for _, sm := range rm.ScopeMetrics {
			metricCount += len(sm.Metrics)
		}
	}
	return fmt.Sprintf("results for %s (%d spans, %d metrics)", short, spanCount, metricCount)
}

// --- preserved helpers (unchanged) ---

type gitErr struct {
	args   []string
	err    error
	stderr string
	code   int
}

func (e *gitErr) Error() string {
	if e.stderr != "" {
		return fmt.Sprintf("git %s: %v: %s", strings.Join(e.args, " "), e.err, e.stderr)
	}
	return fmt.Sprintf("git %s: %v", strings.Join(e.args, " "), e.err)
}

func (e *gitErr) Unwrap() error { return e.err }

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		code := -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		return strings.TrimSpace(stdout.String()), &gitErr{args: args, err: err, stderr: strings.TrimSpace(stderr.String()), code: code}
	}
	return strings.TrimSpace(stdout.String()), nil
}

func resolveTargetURL(opts Options) (string, error) {
	return readOriginURL(opts.RepoDir)
}

func readOriginURL(repoDir string) (string, error) {
	out, err := runGit(repoDir, "remote", "get-url", "origin")
	if err != nil {
		var ge *gitErr
		if errors.As(err, &ge) && ge.code == 2 {
			return "", ErrNoOrigin
		}
		return "", err
	}
	if out == "" {
		return "", ErrNoOrigin
	}
	return out, nil
}

func branchExistsOnRemote(remoteURL, branch string) (bool, error) {
	_, err := runGit("", "ls-remote", "--exit-code", remoteURL, "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	var ge *gitErr
	if errors.As(err, &ge) && ge.code == 2 {
		return false, nil
	}
	return false, fmt.Errorf("ls-remote %s: %w", remoteURL, err)
}

func openOrInitDataRepo(workDir, remoteURL, branch string) (branchExisted bool, err error) {
	exists, err := branchExistsOnRemote(remoteURL, branch)
	if err != nil {
		return false, err
	}
	if exists {
		if err := os.Remove(workDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("clear workdir: %w", err)
		}
		if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
			return false, fmt.Errorf("clone data branch: %w", err)
		}
		if err := configureBotIdentity(workDir); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return false, fmt.Errorf("mkdir workdir: %w", err)
	}
	if _, err := runGit(workDir, "init", "--quiet", "."); err != nil {
		return false, fmt.Errorf("git init: %w", err)
	}
	if _, err := runGit(workDir, "symbolic-ref", "HEAD", "refs/heads/"+branch); err != nil {
		return false, fmt.Errorf("set HEAD: %w", err)
	}
	if _, err := runGit(workDir, "remote", "add", "origin", remoteURL); err != nil {
		return false, fmt.Errorf("add origin: %w", err)
	}
	if err := configureBotIdentity(workDir); err != nil {
		return false, err
	}
	return false, nil
}

func configureBotIdentity(workDir string) error {
	if _, err := runGit(workDir, "config", "user.name", botName); err != nil {
		return fmt.Errorf("config user.name: %w", err)
	}
	if _, err := runGit(workDir, "config", "user.email", botEmail); err != nil {
		return fmt.Errorf("config user.email: %w", err)
	}
	return nil
}

func commitAll(workDir, msg string) error {
	if _, err := runGit(workDir, "add", "."); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if _, err := runGit(workDir, "commit", "--quiet", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

func pushBranch(workDir, branch string) error {
	refspec := fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)
	_, err := runGit(workDir, "push", "--quiet", "origin", refspec)
	return err
}

func pushWithRetry(workDir, branch string, branchExisted bool) error {
	var lastErr error
	for attempt := 1; attempt <= maxPushAttempts; attempt++ {
		err := pushBranch(workDir, branch)
		if err == nil {
			return nil
		}
		lastErr = err
		if !branchExisted || !isNonFastForward(err) {
			return err
		}
		if rebErr := pullRebase(workDir, branch); rebErr != nil {
			return fmt.Errorf("rebase after push conflict (attempt %d): %w", attempt, rebErr)
		}
	}
	return fmt.Errorf("push failed after %d retries: %w", maxPushAttempts, lastErr)
}

func isNonFastForward(err error) bool {
	if err == nil {
		return false
	}
	var ge *gitErr
	if !errors.As(err, &ge) {
		return false
	}
	msg := ge.stderr
	switch {
	case strings.Contains(msg, "non-fast-forward"),
		strings.Contains(msg, "fetch first"),
		strings.Contains(msg, "stale info"),
		strings.Contains(msg, "rejected"):
		return true
	}
	return false
}

func pullRebase(workDir, branch string) error {
	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
	if _, err := runGit(workDir, "fetch", "--quiet", "origin", refspec); err != nil {
		return fmt.Errorf("fetch %s: %w", branch, err)
	}
	target := fmt.Sprintf("refs/remotes/origin/%s", branch)
	if _, err := runGit(workDir, "rebase", target); err != nil {
		_, _ = runGit(workDir, "rebase", "--abort")
		return fmt.Errorf("git rebase %s: %w", target, err)
	}
	return nil
}

func cmdHash(cmd []string) string {
	if len(cmd) == 0 {
		return ""
	}
	h := sha1.Sum([]byte(strings.Join(cmd, "\x00")))
	return hex.EncodeToString(h[:8])
}

func workingTreeStatus(repoDir string) (bool, string) {
	out, err := runGit(repoDir, "status", "--porcelain")
	if err != nil || out == "" {
		return false, ""
	}
	diff, derr := runGit(repoDir, "diff", "HEAD")
	if derr != nil {
		h := sha1.Sum([]byte(out))
		return true, hex.EncodeToString(h[:8])
	}
	h := sha1.Sum([]byte(diff))
	return true, hex.EncodeToString(h[:8])
}

func parsePRFromEnv() int {
	if v := os.Getenv("GITHUB_PR_NUMBER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	ref := os.Getenv("GITHUB_REF")
	if !strings.HasPrefix(ref, "refs/pull/") {
		return 0
	}
	parts := strings.Split(ref, "/")
	if len(parts) < 3 {
		return 0
	}
	n, _ := strconv.Atoi(parts[2])
	return n
}
