package persist

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
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

// Options controls Backend creation.
type Options struct {
	RepoDir    string
	DataBranch string // "" → DefaultDataBranch
	AuthToken  string
	NoRemote   bool
	Dev        bool
}

const DevDir = ".defrost-dev"

// Backend is the swappable persistence layer. Schema 3.
type Backend interface {
	InitialisePersistence() error
	// InsertNewRun atomically persists the root run span, every test span,
	// and every metric data point produced by one defrost exec invocation.
	InsertNewRun(root models.Span, testSpans []models.Span, metrics []models.MetricEntry) error
	// GetTestHistory returns every span persisted under traces/<testName>.ndjson,
	// sorted oldest first by start time. Empty slice when nothing matches.
	GetTestHistory(testName string) ([]models.Span, error)
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
	res := map[string]any{
		"service.name":            "defrost",
		"service.version":         defrostVersion,
		"cicd.pipeline.run.id":    runID,
		"host.os.type":            runtime.GOOS,
		"host.arch":               runtime.GOARCH,
		"process.runtime.version": runtime.Version(),
		"defrost.cmd":             cmd,
		"defrost.cmd_hash":        cmdHash(cmd),
		"defrost.run_id":          runID,
	}

	if out, err := runGit(opts.RepoDir, "log", "-1", "--format=%H%n%P%n%ae%n%an"); err == nil {
		lines := strings.SplitN(out, "\n", 4)
		if len(lines) >= 1 && lines[0] != "" {
			res["vcs.repository.ref.revision"] = lines[0]
		}
		if len(lines) >= 2 {
			parents := strings.Fields(lines[1])
			if len(parents) > 0 {
				res["defrost.parent_commit"] = parents[0]
			}
		}
		if len(lines) >= 3 && lines[2] != "" {
			res["defrost.author_email"] = lines[2]
		}
		if len(lines) >= 4 && lines[3] != "" {
			res["defrost.author_name"] = lines[3]
		}
	}

	if out, err := runGit(opts.RepoDir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && out != "HEAD" && out != "" {
		res["vcs.repository.ref.name"] = out
	} else if v := os.Getenv("GITHUB_HEAD_REF"); v != "" {
		res["vcs.repository.ref.name"] = v
	} else if v := os.Getenv("GITHUB_REF_NAME"); v != "" {
		res["vcs.repository.ref.name"] = v
	}

	if pr := parsePRFromEnv(); pr != 0 {
		res["vcs.repository.change.id"] = strconv.Itoa(pr)
	}

	dirty, dirtyHash := workingTreeStatus(opts.RepoDir)
	res["defrost.dirty"] = dirty
	if dirtyHash != "" {
		res["defrost.dirty_hash"] = dirtyHash
	}

	now := time.Now().UnixNano()
	return models.RunContext{
		RunID:             runID,
		TraceID:           models.DeriveTraceID(runID),
		RootSpanID:        models.NewSpanID(),
		Resource:          res,
		StartTimeUnixNano: now,
	}, nil
}

// NewRootSpan returns the bookkeeping span representing one defrost exec
// invocation. End time and status are filled in by the caller after the
// child exits and persistence either succeeds or fails.
func NewRootSpan(run models.RunContext) models.Span {
	return models.Span{
		Schema:            models.SchemaV3,
		TraceID:           run.TraceID,
		SpanID:            run.RootSpanID,
		Name:              rootSpanName,
		Kind:              "INTERNAL",
		StartTimeUnixNano: run.StartTimeUnixNano,
		Status:            models.SpanStatus{Code: "UNSET"},
		Attributes:        map[string]any{"defrost.run_id": run.RunID},
		Resource:          run.Resource,
	}
}

// gitBackend stores spans and metrics on a dedicated git data branch.
type gitBackend struct{ opts Options }

func (b *gitBackend) InitialisePersistence() error { return nil }

func (b *gitBackend) InsertNewRun(root models.Span, testSpans []models.Span, metrics []models.MetricEntry) error {
	if root.SpanID == "" {
		return errors.New("persist: empty root span id")
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

	if err := appendSpans(workDir, append([]models.Span{root}, testSpans...)); err != nil {
		return err
	}
	if err := appendMetrics(workDir, metrics); err != nil {
		return err
	}

	if err := commitAll(workDir, commitMessage(root, len(testSpans)+1, len(metrics))); err != nil {
		return err
	}

	return pushWithRetry(workDir, branch, branchExisted)
}

func (b *gitBackend) GetTestHistory(testName string) ([]models.Span, error) {
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
	_ = os.Remove(workDir) // clone wants empty path

	if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
		return nil, fmt.Errorf("clone data branch: %w", err)
	}

	return readSpansFromDir(workDir, testName)
}

// fileBackend writes spans/metrics to a plain directory; no git operations.
type fileBackend struct{ dir string }

func (b *fileBackend) InitialisePersistence() error {
	return os.MkdirAll(b.dir, 0o755)
}

func (b *fileBackend) InsertNewRun(root models.Span, testSpans []models.Span, metrics []models.MetricEntry) error {
	if root.SpanID == "" {
		return errors.New("persist: empty root span id")
	}
	if err := b.InitialisePersistence(); err != nil {
		return err
	}
	if err := appendSpans(b.dir, append([]models.Span{root}, testSpans...)); err != nil {
		return err
	}
	return appendMetrics(b.dir, metrics)
}

func (b *fileBackend) GetTestHistory(testName string) ([]models.Span, error) {
	if _, err := os.Stat(b.dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return readSpansFromDir(b.dir, testName)
}

// --- write helpers ---

func writeSeed(workDir string) error {
	if err := os.WriteFile(filepath.Join(workDir, ".gitattributes"), []byte(gitAttributes), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(workDir, "README.md"), []byte(readme), 0o644)
}

func appendSpans(workDir string, spans []models.Span) error {
	if len(spans) == 0 {
		return nil
	}
	dir := filepath.Join(workDir, "traces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, s := range spans {
		line, err := json.Marshal(s)
		if err != nil {
			return fmt.Errorf("marshal span %s: %w", s.Name, err)
		}
		path := filepath.Join(dir, EncodeName(s.Name)+".ndjson")
		if err := appendLine(path, line); err != nil {
			return err
		}
	}
	return nil
}

func appendMetrics(workDir string, entries []models.MetricEntry) error {
	if len(entries) == 0 {
		return nil
	}
	dir := filepath.Join(workDir, "metrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("marshal metric %s: %w", e.Name, err)
		}
		path := filepath.Join(dir, EncodeName(e.Name)+".ndjson")
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

func readSpansFromDir(dir, testName string) ([]models.Span, error) {
	path := filepath.Join(dir, "traces", EncodeName(testName)+".ndjson")
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	spans, err := parseSpansNDJSON(f)
	if err != nil {
		return nil, err
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].StartTimeUnixNano < spans[j].StartTimeUnixNano })
	return spans, nil
}

func parseSpansNDJSON(r io.Reader) ([]models.Span, error) {
	var out []models.Span
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var s models.Span
		if err := json.Unmarshal(line, &s); err != nil {
			return nil, fmt.Errorf("parse ndjson line: %w", err)
		}
		out = append(out, s)
	}
	return out, sc.Err()
}

func commitMessage(root models.Span, nSpans, nMetrics int) string {
	commit, _ := root.Resource["vcs.repository.ref.revision"].(string)
	short := commit
	if len(short) > 7 {
		short = short[:7]
	}
	if short == "" {
		runID, _ := root.Resource["defrost.run_id"].(string)
		short = runID
		if len(short) > 8 {
			short = short[:8]
		}
	}
	return fmt.Sprintf("results for %s (%d spans, %d metrics)", short, nSpans, nMetrics)
}

// --- preserved helpers (unchanged from schema 2) ---

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
