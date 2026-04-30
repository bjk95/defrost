package persist

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/bjk95/defrost/internal/models"
)

const (
	DefaultDataBranch = "_defrost"

	botName  = "defrost[bot]"
	botEmail = "defrost[bot]@users.noreply.github.com"

	gitAttributes = "traces/*.ndjson merge=union\nmetrics/*.ndjson merge=union\n"
	readme        = "# defrost data branch\n\nManaged by the defrost CLI. Do not edit by hand.\n"

	maxPushAttempts = 5

	rootSpanName = "defrost.run"
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
	NoRemote   bool
	Dev        bool
}

const DevDir = ".defrost-dev"

// Backend is the swappable persistence layer. Spans and metrics are
// passed and returned as the canonical OTel proto types — *tracepb.Span,
// *metricspb.Metric — wrapped in their per-record Resource via
// *tracepb.ResourceSpans / *metricspb.ResourceMetrics. On disk each
// line is one ResourceSpans (single Span) or one ResourceMetrics
// (single Metric with one data point).
type Backend interface {
	InitialisePersistence() error

	// InsertNewRun atomically persists everything produced by one
	// defrost exec invocation: the root run span, every test span, and
	// every metric data point. Each ResourceSpans / ResourceMetrics
	// carries its own Resource — span and metric Resources may differ
	// (caller strips run-unique fields from the metric Resource to keep
	// metric cardinality bounded).
	InsertNewRun(traces []*tracepb.ResourceSpans, metrics []*metricspb.ResourceMetrics) error

	// GetTestHistory returns every span persisted under
	// traces/<testName>.ndjson, sorted oldest first by start time. Each
	// element is a ResourceSpans containing exactly one Span. Empty
	// slice when nothing matches.
	GetTestHistory(testName string) ([]*tracepb.ResourceSpans, error)

	// GetSuppressions returns the current list of suppressed test IDs,
	// or an empty slice if none have been recorded. A missing data branch
	// is not an error.
	GetSuppressions() ([]string, error)
	// UpdateSuppressions reads the current list, applies mutate, and
	// writes the result. msg is the commit message used for the update.
	UpdateSuppressions(mutate func([]string) []string, msg string) error

	// LoadAll returns every root run span and every test span across
	// all files, grouped by encoded span name. Used by `defrost serve`
	// to populate the heatmap grid in a single read instead of N.
	// Returns nil slices/maps when there is no data yet.
	LoadAll() (rootSpans []*tracepb.ResourceSpans, byEncodedName map[string][]*tracepb.ResourceSpans, err error)

	// LoadAllMetrics returns every persisted metric data point across
	// all metrics/*.ndjson files. Each element is a ResourceMetrics
	// containing exactly one Metric with one data point — the storage
	// shape produced by InsertNewRun. Returns nil when there is no
	// metrics data on disk.
	LoadAllMetrics() ([]*metricspb.ResourceMetrics, error)
}

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
		models.StringAttr("host.os.type", runtime.GOOS),
		models.StringAttr("host.arch", runtime.GOARCH),
		models.StringAttr("process.runtime.version", runtime.Version()),
		models.StringArrayAttr("defrost.cmd", cmd),
		models.StringAttr("defrost.cmd_hash", cmdHash(cmd)),
		models.StringAttr("defrost.run_id", runID),
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
		"defrost.run_id":             {},
		"cicd.pipeline.run.id":       {},
		"defrost.cmd":                {},
		"defrost.dirty_hash":         {},
		"defrost.author_email":       {},
		"defrost.author_name":        {},
		"defrost.parent_commit":      {},
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
		Attributes:        []*commonpb.KeyValue{models.StringAttr("defrost.run_id", run.RunID)},
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

// WrapSpansInResource constructs one *tracepb.ResourceSpans per span,
// each carrying the same Resource. This is the storage shape: each line
// in traces/<name>.ndjson is one ResourceSpans with a single Span.
func WrapSpansInResource(resource *resourcepb.Resource, spans []*tracepb.Span) []*tracepb.ResourceSpans {
	if len(spans) == 0 {
		return nil
	}
	out := make([]*tracepb.ResourceSpans, 0, len(spans))
	for _, s := range spans {
		out = append(out, &tracepb.ResourceSpans{
			Resource: resource,
			ScopeSpans: []*tracepb.ScopeSpans{{
				Scope: &commonpb.InstrumentationScope{Name: "defrost"},
				Spans: []*tracepb.Span{s},
			}},
		})
	}
	return out
}

// WrapMetricsInResource constructs one *metricspb.ResourceMetrics per
// metric, each carrying the same Resource. This is the storage shape:
// each line in metrics/<name>.ndjson is one ResourceMetrics with a
// single Metric (which itself contains one data point).
func WrapMetricsInResource(resource *resourcepb.Resource, metrics []*metricspb.Metric) []*metricspb.ResourceMetrics {
	if len(metrics) == 0 {
		return nil
	}
	out := make([]*metricspb.ResourceMetrics, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, &metricspb.ResourceMetrics{
			Resource: resource,
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Scope:   &commonpb.InstrumentationScope{Name: "defrost"},
				Metrics: []*metricspb.Metric{m},
			}},
		})
	}
	return out
}

// SpanFromResourceSpans returns the single *tracepb.Span inside a
// ResourceSpans we wrote. Panics if the ResourceSpans is empty (we never
// write empty wrappers).
func SpanFromResourceSpans(rs *tracepb.ResourceSpans) *tracepb.Span {
	if rs == nil || len(rs.ScopeSpans) == 0 || len(rs.ScopeSpans[0].Spans) == 0 {
		return nil
	}
	return rs.ScopeSpans[0].Spans[0]
}

// MetricFromResourceMetrics returns the single *metricspb.Metric inside
// a ResourceMetrics we wrote.
func MetricFromResourceMetrics(rm *metricspb.ResourceMetrics) *metricspb.Metric {
	if rm == nil || len(rm.ScopeMetrics) == 0 || len(rm.ScopeMetrics[0].Metrics) == 0 {
		return nil
	}
	return rm.ScopeMetrics[0].Metrics[0]
}

// gitBackend stores spans and metrics on a dedicated git data branch.
type gitBackend struct{ opts Options }

func (b *gitBackend) InitialisePersistence() error { return nil }

func (b *gitBackend) InsertNewRun(traces []*tracepb.ResourceSpans, metrics []*metricspb.ResourceMetrics) error {
	if len(traces) == 0 && len(metrics) == 0 {
		return nil
	}

	branch := b.opts.DataBranch
	if branch == "" {
		branch = DefaultDataBranch
	}

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

	if err := appendSpans(workDir, traces); err != nil {
		return err
	}
	if err := appendMetrics(workDir, metrics); err != nil {
		return err
	}

	if err := commitAll(workDir, commitMessage(traces, metrics)); err != nil {
		return err
	}

	return pushWithRetry(workDir, branch, branchExisted)
}

func (b *gitBackend) GetTestHistory(testName string) ([]*tracepb.ResourceSpans, error) {
	branch := b.opts.DataBranch
	if branch == "" {
		branch = DefaultDataBranch
	}
	remoteURL, err := resolveTargetURL(b.opts)
	if err != nil {
		return nil, err
	}
	exists, err := branchExistsOnRemote(remoteURL, branch)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	workDir, err := os.MkdirTemp("", "defrost-read-")
	if err != nil {
		return nil, fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(workDir)
	_ = os.Remove(workDir)

	if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
		return nil, fmt.Errorf("clone data branch: %w", err)
	}

	return readSpansFromDir(workDir, testName)
}

func (b *gitBackend) LoadAll() ([]*tracepb.ResourceSpans, map[string][]*tracepb.ResourceSpans, error) {
	branch := b.opts.DataBranch
	if branch == "" {
		branch = DefaultDataBranch
	}
	remoteURL, err := resolveTargetURL(b.opts)
	if err != nil {
		return nil, nil, err
	}
	exists, err := branchExistsOnRemote(remoteURL, branch)
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		return nil, nil, nil
	}

	workDir, err := os.MkdirTemp("", "defrost-read-")
	if err != nil {
		return nil, nil, fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(workDir)
	_ = os.Remove(workDir)

	if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
		return nil, nil, fmt.Errorf("clone data branch: %w", err)
	}
	return readAllSpans(workDir)
}

func (b *gitBackend) LoadAllMetrics() ([]*metricspb.ResourceMetrics, error) {
	branch := b.opts.DataBranch
	if branch == "" {
		branch = DefaultDataBranch
	}
	remoteURL, err := resolveTargetURL(b.opts)
	if err != nil {
		return nil, err
	}
	exists, err := branchExistsOnRemote(remoteURL, branch)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	workDir, err := os.MkdirTemp("", "defrost-read-")
	if err != nil {
		return nil, fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(workDir)
	_ = os.Remove(workDir)

	if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
		return nil, fmt.Errorf("clone data branch: %w", err)
	}
	return readAllMetrics(workDir)
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
	if err := appendSpans(b.dir, traces); err != nil {
		return err
	}
	return appendMetrics(b.dir, metrics)
}

func (b *fileBackend) GetTestHistory(testName string) ([]*tracepb.ResourceSpans, error) {
	if _, err := os.Stat(b.dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return readSpansFromDir(b.dir, testName)
}

func (b *fileBackend) LoadAll() ([]*tracepb.ResourceSpans, map[string][]*tracepb.ResourceSpans, error) {
	if _, err := os.Stat(b.dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	return readAllSpans(b.dir)
}

func (b *fileBackend) LoadAllMetrics() ([]*metricspb.ResourceMetrics, error) {
	if _, err := os.Stat(b.dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return readAllMetrics(b.dir)
}

// --- write helpers ---

// protoMarshalCompact writes a single-line JSON encoding of a proto
// message using the canonical OTLP/JSON form. We strip whitespace so each
// line is one record (NDJSON discipline).
var protoMarshal = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}

func writeSeed(workDir string) error {
	if err := os.WriteFile(filepath.Join(workDir, ".gitattributes"), []byte(gitAttributes), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(workDir, "README.md"), []byte(readme), 0o644)
}

func appendSpans(workDir string, traces []*tracepb.ResourceSpans) error {
	if len(traces) == 0 {
		return nil
	}
	dir := filepath.Join(workDir, "traces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, rs := range traces {
		span := SpanFromResourceSpans(rs)
		if span == nil {
			continue
		}
		line, err := protoMarshal.Marshal(rs)
		if err != nil {
			return fmt.Errorf("marshal span %s: %w", span.Name, err)
		}
		line = bytes.ReplaceAll(line, []byte("\n"), []byte(" "))
		path := filepath.Join(dir, EncodeName(span.Name)+".ndjson")
		if err := appendLine(path, line); err != nil {
			return err
		}
	}
	return nil
}

func appendMetrics(workDir string, metrics []*metricspb.ResourceMetrics) error {
	if len(metrics) == 0 {
		return nil
	}
	dir := filepath.Join(workDir, "metrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, rm := range metrics {
		m := MetricFromResourceMetrics(rm)
		if m == nil {
			continue
		}
		line, err := protoMarshal.Marshal(rm)
		if err != nil {
			return fmt.Errorf("marshal metric %s: %w", m.Name, err)
		}
		line = bytes.ReplaceAll(line, []byte("\n"), []byte(" "))
		path := filepath.Join(dir, EncodeName(m.Name)+".ndjson")
		if err := appendLine(path, line); err != nil {
			return err
		}
	}
	return nil
}

func appendLine(path string, line []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	_, werr := f.Write(append(line, '\n'))
	cerr := f.Close()
	if werr != nil {
		return fmt.Errorf("write %s: %w", path, werr)
	}
	if cerr != nil {
		return fmt.Errorf("close %s: %w", path, cerr)
	}
	return nil
}

// --- read helpers ---

func readSpansFromDir(dir, testName string) ([]*tracepb.ResourceSpans, error) {
	path := filepath.Join(dir, "traces", EncodeName(testName)+".ndjson")
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	traces, err := parseSpansNDJSON(f)
	if err != nil {
		return nil, err
	}
	sort.Slice(traces, func(i, j int) bool {
		a := SpanFromResourceSpans(traces[i])
		b := SpanFromResourceSpans(traces[j])
		if a == nil || b == nil {
			return false
		}
		return a.StartTimeUnixNano < b.StartTimeUnixNano
	})
	return traces, nil
}

func readAllSpans(dir string) ([]*tracepb.ResourceSpans, map[string][]*tracepb.ResourceSpans, error) {
	tracesDir := filepath.Join(dir, "traces")
	files, err := os.ReadDir(tracesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	rootFile := EncodeName(rootSpanName) + ".ndjson"
	var roots []*tracepb.ResourceSpans
	byEncodedName := make(map[string][]*tracepb.ResourceSpans)
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".ndjson") {
			continue
		}
		full := filepath.Join(tracesDir, f.Name())
		fh, err := os.Open(full)
		if err != nil {
			return nil, nil, err
		}
		traces, err := parseSpansNDJSON(fh)
		fh.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", full, err)
		}
		if f.Name() == rootFile {
			roots = append(roots, traces...)
			continue
		}
		key := strings.TrimSuffix(f.Name(), ".ndjson")
		byEncodedName[key] = traces
	}
	return roots, byEncodedName, nil
}

func parseSpansNDJSON(r io.Reader) ([]*tracepb.ResourceSpans, error) {
	var out []*tracepb.ResourceSpans
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		rs := &tracepb.ResourceSpans{}
		if err := protojson.Unmarshal(line, rs); err != nil {
			return nil, fmt.Errorf("parse ndjson line: %w", err)
		}
		out = append(out, rs)
	}
	return out, sc.Err()
}

func readAllMetrics(dir string) ([]*metricspb.ResourceMetrics, error) {
	metricsDir := filepath.Join(dir, "metrics")
	files, err := os.ReadDir(metricsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []*metricspb.ResourceMetrics
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".ndjson") {
			continue
		}
		full := filepath.Join(metricsDir, f.Name())
		fh, err := os.Open(full)
		if err != nil {
			return nil, err
		}
		records, err := parseMetricsNDJSON(fh)
		fh.Close()
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", full, err)
		}
		out = append(out, records...)
	}
	return out, nil
}

func parseMetricsNDJSON(r io.Reader) ([]*metricspb.ResourceMetrics, error) {
	var out []*metricspb.ResourceMetrics
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		rm := &metricspb.ResourceMetrics{}
		if err := protojson.Unmarshal(line, rm); err != nil {
			return nil, fmt.Errorf("parse ndjson line: %w", err)
		}
		out = append(out, rm)
	}
	return out, sc.Err()
}

func commitMessage(traces []*tracepb.ResourceSpans, metrics []*metricspb.ResourceMetrics) string {
	short := ""
	for _, rs := range traces {
		span := SpanFromResourceSpans(rs)
		if span == nil || span.Name != rootSpanName {
			continue
		}
		commit := models.ResourceString(rs.Resource, "vcs.repository.ref.revision")
		if commit != "" {
			short = commit
			if len(short) > 7 {
				short = short[:7]
			}
			break
		}
		runID := models.ResourceString(rs.Resource, "defrost.run_id")
		if runID != "" {
			short = runID
			if len(short) > 8 {
				short = short[:8]
			}
		}
	}
	if short == "" {
		short = "unknown"
	}
	return fmt.Sprintf("results for %s (%d spans, %d metrics)", short, len(traces), len(metrics))
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
	if opts.NoRemote {
		return localGitDir(opts.RepoDir)
	}
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

func localGitDir(repoDir string) (string, error) {
	out, err := runGit(repoDir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(repoDir, out)
	}
	return filepath.Abs(out)
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
