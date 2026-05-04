// Package persist is the on-disk side of the defrost OTel pipeline.
//
// `gitBackend.InsertNewRun` (used in normal mode) takes pre-serialized
// canonical OTLP/protobuf bytes for traces, metrics, and logs and writes
// them as zstd-compressed files under traces/, metrics/, and logs/ on a
// dedicated git data branch. `fileBackend` (used in --dev mode) writes
// the same files into a local scratch directory without any git
// operations.
//
// The bytes-on-disk shape is canonical OTLP. Serialization happens in
// `internal/runner` (via `ptraceotlp.MarshalProto` etc.) so the on-disk
// format matches what an upstream OTel Collector exporter would have
// produced; downstream readers (the local DuckDB hydrator, future
// hosted ClickHouse) can decode without translation.
package persist

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/bjk95/defrost/internal/models"
)

const (
	DefaultDataBranch = "_defrost"

	botName  = "defrost[bot]"
	botEmail = "defrost[bot]@users.noreply.github.com"

	readme = "# defrost data branch\n\nManaged by the defrost CLI. Do not edit by hand.\n"

	maxPushAttempts = 5

	// fileSuffix is appended to every per-trace, per-run-metrics and
	// per-run-logs file on disk. Zstd-compressed canonical OTLP/Protobuf.
	fileSuffix = ".otlp.pb.zst"
)

// Options controls Backend creation.
type Options struct {
	RepoDir    string
	DataBranch string // "" → DefaultDataBranch
	AuthToken  string
	Dev        bool
}


// Run is the bundle of pre-serialized canonical OTLP bytes that one
// `defrost exec` invocation flushes to disk. Any of TraceBytes,
// MetricsBytes, or LogsBytes may be empty — empty signals are skipped
// at write time.
//
// TraceID is the raw 16-byte OTel trace id, used to derive the file
// name for every signal. RunStartUTC is the wall-clock UTC time the
// run started; used for the YYYY/MM/DD partition under each signal
// directory.
type Run struct {
	TraceID      [16]byte
	RunStartUTC  time.Time
	TraceBytes   []byte
	MetricsBytes []byte
	LogsBytes    []byte
	// Commit subject for the data-branch commit. Optional; the backend
	// generates a default when empty.
	CommitMessage string
}

// Backend is the swappable persistence layer. Spans, metrics, and logs
// are passed in as canonical OTLP/Protobuf bytes and stored
// zstd-compressed as one file per signal per `defrost exec` invocation,
// named by the run's trace_id and partitioned by run-start UTC date.
type Backend interface {
	// InsertNewRun atomically persists everything produced by one
	// defrost exec invocation. Empty signals are skipped — the file is
	// not written.
	InsertNewRun(run Run) error

	// GetSuppressions returns the current list of suppressed test IDs,
	// or an empty slice if none have been recorded. A missing data branch
	// is not an error.
	GetSuppressions() ([]string, error)

	// UpdateSuppressions reads the current list, applies mutate, and
	// writes the result. msg is the commit message used for the update.
	UpdateSuppressions(mutate func([]string) []string, msg string) error

	// RemoteHeadSHA returns the commit SHA at the tip of the data
	// branch on origin. Returns ("", nil) when the branch doesn't
	// exist on origin; ("", nil) in dev mode (no remote).
	//
	// Used by the read path as a cheap freshness probe — when the
	// returned SHA matches a previously-hydrated SHA, the caller can
	// skip CloneForRead and the file walk entirely. One HTTPS round
	// trip via `git ls-remote` (~50ms typical), no working tree
	// involved.
	RemoteHeadSHA() (string, error)

	// CloneForRead ensures a local working tree of the data branch is
	// present and current, then returns the snapshot identity. In
	// git mode the working tree is persistent at
	// $UserCacheDir/defrost/<repo-hash>/data; the first call clones
	// into it, subsequent calls run `git fetch` + `git reset --hard`
	// against origin. snap.Dir is "" when there's no data yet (branch
	// missing on origin, or scratch dir absent in dev mode).
	//
	// snap.Reset is true when the persistent cache was force-reset to
	// match a rewritten remote (e.g. after `defrost drop history`
	// pushes an orphan commit). Callers maintaining derived state
	// must drop and rebuild it when Reset is true.
	CloneForRead() (snap Snapshot, err error)

	// DropHistory inventories the data branch and (after confirm
	// returns true) rewrites it via a single orphan commit force-pushed
	// with --force-with-lease.
	DropHistory(sel DropSelector, confirm func(DropPlan) bool) error
}

// New returns the Backend implied by opts. Dev mode selects the local
// file backend (no remote pushes); otherwise the git-data-branch
// backend is used. Both backends share the same on-disk layout under
// <repoDir>/.defrost/.
func New(opts Options) Backend {
	if opts.Dev {
		return &fileBackend{opts: opts, dir: LocalRoot(opts)}
	}
	return &gitBackend{opts: opts}
}

// ErrNoOrigin is returned when the user's repo has no origin remote configured.
var ErrNoOrigin = errors.New("no origin remote configured")

// EncodeName escapes a span or metric name into a filesystem-safe
// segment. Reversible via DecodeName.
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

// DetectRunContext builds a RunContext for one defrost exec invocation
// by inspecting the user's repo, environment variables, and Go runtime.
// Attribute keys follow OTel CI/CD and VCS semantic conventions where
// applicable; defrost-private keys use a `defrost.*` prefix.
//
// Returns the raw RunContext (primitive []models.Attr; no pdata
// dependency). The conversion to pcommon.Map happens at the boundary
// in internal/runner.
func DetectRunContext(opts Options, cmd []string, defrostVersion string) (models.RunContext, error) {
	if _, err := runGit(opts.RepoDir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return models.RunContext{}, fmt.Errorf("not a git repo at %s: %w", opts.RepoDir, err)
	}

	runID := NewRunID()
	attrs := []models.Attr{
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
		Attrs:             attrs,
		StartTimeUnixNano: now,
	}, nil
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

// RepoPrefix returns the path-from-repo-root for cmd's working
// directory, used by `runner.RunFQN` when constructing the auto-emitted
// run-duration metric name. Empty string when invocation is at the repo
// root or when the prefix lookup fails.
func RepoPrefix(repoDir string) string {
	prefix, err := runGit(repoDir, "rev-parse", "--show-prefix")
	if err != nil {
		return ""
	}
	return prefix
}

// gitBackend stores spans, metrics, and logs on a dedicated git data branch.
type gitBackend struct{ opts Options }

func (b *gitBackend) dataBranch() string {
	if b.opts.DataBranch != "" {
		return b.opts.DataBranch
	}
	return DefaultDataBranch
}

func (b *gitBackend) InsertNewRun(run Run) error {
	if !run.hasAny() {
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
	if err := writeRunFiles(workDir, run); err != nil {
		return err
	}
	msg := run.CommitMessage
	if msg == "" {
		msg = run.defaultCommitMessage()
	}
	if err := commitAll(workDir, msg); err != nil {
		return err
	}
	return pushWithRetry(workDir, branch, branchExisted)
}

// CloneForRead ensures a persistent local working tree of the data
// branch at $UserCacheDir/defrost/<repo-hash>/data, fetching new
// commits since the previous call rather than re-cloning. See the
// Backend interface doc for the snapshot contract.
//
// Force-reset detection: a healthy fetch fast-forwards the local
// `<branch>` to the remote tip. If that fails (the remote was rewritten
// — `defrost drop history` does this), we wipe the worktree and clone
// from scratch, returning Snapshot.Reset=true so callers know to drop
// any derived state.
func (b *gitBackend) CloneForRead() (Snapshot, error) {
	branch := b.dataBranch()
	remoteURL, err := resolveTargetURL(b.opts)
	if err != nil {
		return Snapshot{}, err
	}
	exists, err := branchExistsOnRemote(remoteURL, branch)
	if err != nil {
		return Snapshot{}, err
	}
	if !exists {
		return Snapshot{}, nil
	}

	// Make sure the user's main repo .gitignore knows about
	// .defrost/ before we materialise the worktree there. Best-effort —
	// failures don't prevent reads.
	_ = EnsureUserRepoIgnoresDefrost(b.opts)
	workDir, err := b.dataCacheDir()
	if err != nil {
		return Snapshot{}, err
	}
	if err := os.MkdirAll(filepath.Dir(workDir), 0o755); err != nil {
		return Snapshot{}, fmt.Errorf("mkdir cache parent: %w", err)
	}

	// Cold path: no working tree yet. Full shallow clone into the
	// cache dir; SHA from HEAD; Reset=false (first hydrate of this
	// cache, by definition not a force-reset).
	if _, err := os.Stat(filepath.Join(workDir, ".git")); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return Snapshot{}, err
		}
		if err := removeAllPreservingParent(workDir); err != nil {
			return Snapshot{}, fmt.Errorf("clear stale cache dir: %w", err)
		}
		if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
			return Snapshot{}, fmt.Errorf("clone data branch: %w", err)
		}
		return Snapshot{Dir: workDir, SHA: localHeadSHA(workDir)}, nil
	}

	// Warm path: a working tree already exists. Try a fetch+reset; if
	// the remote tip isn't reachable (force-push detected via
	// non-existent ref or shallow reject), fall back to a wipe-and-
	// re-clone with Reset=true.
	if reset, err := b.refreshWorktree(workDir, branch); err == nil {
		return Snapshot{Dir: workDir, SHA: localHeadSHA(workDir), Reset: reset}, nil
	}

	// Reconcile by wiping and re-cloning. Anything that was on disk
	// belonged to the old branch; the caller will see Reset=true and
	// drop derived state.
	if err := removeAllPreservingParent(workDir); err != nil {
		return Snapshot{}, fmt.Errorf("wipe cache dir: %w", err)
	}
	if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
		return Snapshot{}, fmt.Errorf("re-clone data branch: %w", err)
	}
	return Snapshot{Dir: workDir, SHA: localHeadSHA(workDir), Reset: true}, nil
}

// refreshWorktree updates dir to the current remote tip. Returns
// (reset, nil) on success — reset=true when the local branch could
// not be fast-forwarded to the remote tip (i.e. the remote was
// rewritten via force-push, typically by `defrost drop history`).
//
// Strategy: try a non-force fetch first. Git rejects non-fast-forward
// updates by default, so a successful fetch implies the update was a
// FF and the caller's derived state is still valid. A failed fetch
// implies the remote diverged — we then force-fetch to reconcile the
// working tree and report Reset=true so the caller drops derived
// state. This works regardless of clone depth (no merge-base ancestor
// walk needed).
//
// Returns a non-nil error when the working tree appears unrecoverable
// (corrupt .git, missing remote, etc.) so the caller can fall back to
// wipe-and-re-clone.
func (b *gitBackend) refreshWorktree(dir, branch string) (bool, error) {
	target := fmt.Sprintf("refs/remotes/origin/%s", branch)
	plainRefspec := fmt.Sprintf("refs/heads/%s:%s", branch, target)
	forceRefspec := "+" + plainRefspec

	// Attempt a fast-forward fetch. Errors here are expected and
	// benign — they signal that the remote wasn't a FF.
	reset := false
	if _, err := runGit(dir, "fetch", "--quiet", "origin", plainRefspec); err != nil {
		// Force the fetch to reconcile, then tell the caller their
		// derived state is stale.
		if _, err2 := runGit(dir, "fetch", "--quiet", "origin", forceRefspec); err2 != nil {
			return false, fmt.Errorf("fetch (force): %w", err2)
		}
		reset = true
	}

	if _, err := runGit(dir, "reset", "--hard", "--quiet", target); err != nil {
		return false, fmt.Errorf("reset: %w", err)
	}
	return reset, nil
}

// fileBackend writes spans/metrics/logs to a plain directory; no git operations.
type fileBackend struct {
	opts Options
	dir  string
}

func (b *fileBackend) InsertNewRun(run Run) error {
	if !run.hasAny() {
		return nil
	}
	// Best-effort: keep .defrost/ out of the user's main repo commits.
	_ = EnsureUserRepoIgnoresDefrost(b.opts)
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return err
	}
	return writeRunFiles(b.dir, run)
}

// CloneForRead in dev mode returns the scratch dir directly. SHA is
// "" because there's no commit identity; Reset is always false because
// the dev backend never gets force-reset (drop history just deletes
// files in place).
func (b *fileBackend) CloneForRead() (Snapshot, error) {
	if _, err := os.Stat(b.dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Snapshot{}, nil
		}
		return Snapshot{}, err
	}
	return Snapshot{Dir: b.dir}, nil
}

// hasAny reports whether the run has at least one signal to write.
func (r Run) hasAny() bool {
	return len(r.TraceBytes) > 0 || len(r.MetricsBytes) > 0 || len(r.LogsBytes) > 0
}

func (r Run) defaultCommitMessage() string {
	signals := make([]string, 0, 3)
	if len(r.TraceBytes) > 0 {
		signals = append(signals, "traces")
	}
	if len(r.MetricsBytes) > 0 {
		signals = append(signals, "metrics")
	}
	if len(r.LogsBytes) > 0 {
		signals = append(signals, "logs")
	}
	if len(signals) == 0 {
		return "defrost: empty run"
	}
	return fmt.Sprintf("defrost: %s for %s", strings.Join(signals, "+"), hex.EncodeToString(r.TraceID[:8]))
}

// SignalDirs returns the directory names persisted under the data
// branch root for each signal. Used by drop.go and the Querier
// hydrator.
var SignalDirs = []string{"traces", "metrics", "logs"}

// --- on-disk write path ---

// zstdEncoder is shared across writes. Default compression level is
// fine for text-heavy proto payloads with run metadata.
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

// SharedZstdDecoder returns the package-level zstd decoder. Exposed so
// the Querier hydrator can read the same files we write without owning
// its own decoder.
func SharedZstdDecoder() (*zstd.Decoder, error) {
	zstdDecoderOnce.Do(func() {
		zstdDecoder, zstdDecoderErr = zstd.NewReader(nil)
	})
	return zstdDecoder, zstdDecoderErr
}

// writeSeed writes the README and data-branch .gitignore at branch
// creation time. The .gitignore keeps the per-machine DuckDB cache
// out of commits while letting it share the worktree at
// <repo>/.defrost/.
func writeSeed(workDir string) error {
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte(readme), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(workDir, ".gitignore"), []byte(dataBranchGitignore), 0o644)
}

// writeRunFiles is the single entry point used by both backends.
// dataDir is the data/ subdirectory under the worktree root —
// <worktree>/data — where signals are partitioned. Writes at most one
// zstd-compressed canonical OTLP/protobuf file per signal,
// partitioned by run-start UTC date.
func writeRunFiles(dataDir string, run Run) error {
	if len(run.TraceBytes) > 0 {
		if err := writeSignalFile(dataDir, "traces", run.TraceID, run.RunStartUTC, run.TraceBytes); err != nil {
			return fmt.Errorf("write traces: %w", err)
		}
	}
	if len(run.MetricsBytes) > 0 {
		if err := writeSignalFile(dataDir, "metrics", run.TraceID, run.RunStartUTC, run.MetricsBytes); err != nil {
			return fmt.Errorf("write metrics: %w", err)
		}
	}
	if len(run.LogsBytes) > 0 {
		if err := writeSignalFile(dataDir, "logs", run.TraceID, run.RunStartUTC, run.LogsBytes); err != nil {
			return fmt.Errorf("write logs: %w", err)
		}
	}
	return nil
}

func writeSignalFile(dataDir, signal string, traceID [16]byte, runStart time.Time, raw []byte) error {
	enc, err := sharedZstdEncoder()
	if err != nil {
		return fmt.Errorf("zstd encoder: %w", err)
	}
	compressed := enc.EncodeAll(raw, nil)
	return writeFileAtomic(SignalPath(dataDir, signal, traceID, runStart), compressed)
}

// SignalPath returns the date-partitioned absolute path for one run's
// signal file: <root>/<signal>/<YYYY>/<MM>/<DD>/<hex trace_id>.otlp.pb.zst.
func SignalPath(root, signal string, traceID [16]byte, runStart time.Time) string {
	id := hex.EncodeToString(traceID[:])
	if runStart.IsZero() {
		runStart = time.Now().UTC()
	}
	y, m, d := runStart.UTC().Date()
	return filepath.Join(root, signal,
		fmt.Sprintf("%04d", y),
		fmt.Sprintf("%02d", int(m)),
		fmt.Sprintf("%02d", d),
		id+fileSuffix,
	)
}

// FileSuffix is the extension every persisted signal file ends with.
// Exported so the Querier hydrator can identify defrost files when
// walking the data branch tree.
const FileSuffix = fileSuffix

// writeFileAtomic writes data to path atomically: write a sibling
// .tmp, fsync the file, rename onto the final name, then fsync the
// parent dir so the rename itself survives a crash.
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

// ListSignalFiles returns every <root>/<signal>/<YYYY>/<MM>/<DD>/<id>.otlp.pb.zst
// path, in arbitrary order. Returns an empty slice if root does not
// exist. Used by the Querier hydrator.
func ListSignalFiles(root, signal string) ([]string, error) {
	dir := filepath.Join(root, signal)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), fileSuffix) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	return out, err
}

// ReadSignalBytes reads and zstd-decompresses one signal file written
// by writeSignalFile. The returned bytes are canonical OTLP/protobuf
// — what `ptraceotlp.UnmarshalProto` (etc.) expects.
func ReadSignalBytes(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec, err := SharedZstdDecoder()
	if err != nil {
		return nil, err
	}
	return dec.DecodeAll(raw, nil)
}

// --- git helpers ---

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
	// Disable signing in the temp workdir. The defrost[bot] identity
	// isn't a signing key, and inheriting a global commit.gpgsign=true
	// (or gpg.format=ssh / etc.) would cause every commit to fail —
	// not just in test environments but in production setups where the
	// user signs their own commits.
	if _, err := runGit(workDir, "config", "commit.gpgsign", "false"); err != nil {
		return fmt.Errorf("config commit.gpgsign: %w", err)
	}
	if _, err := runGit(workDir, "config", "tag.gpgsign", "false"); err != nil {
		return fmt.Errorf("config tag.gpgsign: %w", err)
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
