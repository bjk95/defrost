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
	// DefaultDataBranch is the branch name we use to namespace defrost
	// data on the user's repo. Underscore prefix to (a) make the branch
	// obviously not user-authored and (b) sort it away from real
	// branches in `git branch -a`. Note: git-check-ref-format forbids
	// branch components starting with `.`, so `.defrost` is not a legal
	// alternative.
	DefaultDataBranch = "_defrost"
	SchemaVersion     = 2

	botName  = "defrost[bot]"
	botEmail = "defrost[bot]@users.noreply.github.com"

	gitAttributes = "tests/*.ndjson merge=union\n"
	readme        = "# defrost data branch\n\nManaged by the defrost CLI. Do not edit by hand.\n"

	maxPushAttempts = 5
)

// Entry is one persisted line per (test, run) in tests/<id>.ndjson on the
// data branch. Run-level metadata (commit, author, dirty, etc.) is stored
// once per run in a sibling RunRecord at runs/<run_id>.json — Entry just
// references it via RunID.
type Entry struct {
	Schema     int    `json:"schema"`
	TestID     string `json:"test_id"`
	TestName   string `json:"test_name"`
	RunID      string `json:"run_id"`
	Timestamp  string `json:"ts"`
	Ran        bool   `json:"ran"`
	Passed     bool   `json:"passed"`
	Status     string `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	Output     string `json:"output,omitempty"`
}

// RunRecord captures everything common to all tests in a single defrost
// invocation: which commit, who authored it, was the workspace dirty, what
// command was run, and the runtime environment. Persisted as one file at
// runs/<run_id>.json — unique filename per run, so concurrent writers
// never collide on it.
type RunRecord struct {
	Schema      int      `json:"schema"`
	RunID       string   `json:"run_id"`
	Commit      string   `json:"commit,omitempty"`
	Parent      string   `json:"parent,omitempty"`
	Branch      string   `json:"branch,omitempty"`
	PR          int      `json:"pr,omitempty"`
	AuthorEmail string   `json:"author_email,omitempty"`
	AuthorName  string   `json:"author_name,omitempty"`
	Dirty       bool     `json:"dirty"`
	DirtyHash   string   `json:"dirty_hash,omitempty"`
	Cmd         []string `json:"cmd,omitempty"`
	CmdHash     string   `json:"cmd_hash,omitempty"`
	GoVersion   string   `json:"go_version,omitempty"`
	OS          string   `json:"os,omitempty"`
	Arch        string   `json:"arch,omitempty"`
	Timestamp   string   `json:"ts"`
}

// HistoricalEntry pairs a test's persisted Entry with the RunRecord it
// references. Output of History.
type HistoricalEntry struct {
	Test Entry     `json:"test"`
	Run  RunRecord `json:"run"`
}

// Options controls Persist and History.
type Options struct {
	RepoDir    string // path to the user's git repo
	DataBranch string // branch name on origin; "" → DefaultDataBranch
	AuthToken  string // optional GITHUB_TOKEN; pushed via http.extraHeader for HTTPS remotes

	// NoRemote, when true, persists into the user's local .git directory
	// instead of cloning from / pushing to origin. The data branch lives
	// in the user's repo and is never pushed anywhere. Useful for purely
	// local flaky-test tracking.
	NoRemote bool

	// Dev, when true, selects the dev-mode backend: writes the run
	// record and entry files to <RepoDir>/.defrost-dev/ (a scratch
	// directory the user gitignores) and skips all git operations.
	// Intended for developing defrost itself without polluting the
	// data branch.
	Dev bool
}

// DevDir is the subdirectory of RepoDir used when Dev is set.
const DevDir = ".defrost-dev"

// Backend is the swappable persistence layer. Implementations decide
// where run records and test entries live (git data branch, scratch dir,
// SQL database, etc.). All implementations are responsible for atomicity
// within a single InsertNewTestResults call: either every result lands
// or none does.
type Backend interface {
	// InitialisePersistence prepares storage for first use. Idempotent.
	// Implementations may be lazy — InsertNewTestResults and
	// GetTestHistory must work without an explicit prior call.
	InitialisePersistence() error

	// InsertNewTestResults atomically writes a RunRecord plus one entry
	// per result. Empty results are a no-op.
	InsertNewTestResults(run RunRecord, results []models.TestResult) error

	// GetTestHistory returns every persisted entry for the named test,
	// joined with its RunRecord, oldest first. Returns an empty slice
	// when the test has no history yet.
	GetTestHistory(testName string) ([]HistoricalEntry, error)
}

// New returns the Backend implied by opts. Dev mode selects the local
// scratch backend; otherwise the git-data-branch backend is used.
func New(opts Options) Backend {
	if opts.Dev {
		return &fileBackend{dir: filepath.Join(opts.RepoDir, DevDir)}
	}
	return &gitBackend{opts: opts}
}

// ErrNoOrigin is returned when the user's repo has no origin remote
// configured. Callers should treat this as a soft "nothing to persist
// against" rather than a failure.
var ErrNoOrigin = errors.New("no origin remote configured")

// EncodeTestID returns a filesystem-safe identifier for a Go test name
// such as "github.com/bjk95/defrost/internal/x/TestFoo". Reversible via
// DecodeTestID. Cross-platform — every byte unsafe in a URL path segment
// becomes %XX, so the result handles Windows-reserved characters and
// unicode the same way everywhere.
func EncodeTestID(name string) string {
	return url.PathEscape(name)
}

// DecodeTestID is the inverse of EncodeTestID.
func DecodeTestID(id string) (string, error) {
	return url.PathUnescape(id)
}

// NewRunID returns a sortable-by-time, collision-resistant run identifier.
// Format: <16 hex of UnixNano>-<8 hex of crypto/rand>. ASCII-only, fixed
// width, lex-sortable in time order.
func NewRunID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return fmt.Sprintf("%016x-%s", time.Now().UnixNano(), hex.EncodeToString(buf[:]))
}

// DetectRun builds a RunRecord by inspecting the user's repo, environment
// variables (GitHub Actions), and Go runtime. The caller may freely
// override any field on the returned record before passing it to Persist.
func DetectRun(opts Options, cmd []string) (RunRecord, error) {
	if _, err := runGit(opts.RepoDir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return RunRecord{}, fmt.Errorf("not a git repo at %s: %w", opts.RepoDir, err)
	}

	run := RunRecord{
		Schema:    SchemaVersion,
		RunID:     NewRunID(),
		Cmd:       cmd,
		CmdHash:   cmdHash(cmd),
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	// Read commit hash, parents, author email, author name in one log call.
	if out, err := runGit(opts.RepoDir, "log", "-1", "--format=%H%n%P%n%ae%n%an"); err == nil {
		lines := strings.SplitN(out, "\n", 4)
		if len(lines) >= 1 {
			run.Commit = lines[0]
		}
		if len(lines) >= 2 {
			parents := strings.Fields(lines[1])
			if len(parents) > 0 {
				run.Parent = parents[0]
			}
		}
		if len(lines) >= 3 {
			run.AuthorEmail = lines[2]
		}
		if len(lines) >= 4 {
			run.AuthorName = lines[3]
		}
	}

	if out, err := runGit(opts.RepoDir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && out != "HEAD" {
		run.Branch = out
	}
	if run.Branch == "" {
		if v := os.Getenv("GITHUB_HEAD_REF"); v != "" {
			run.Branch = v
		} else if v := os.Getenv("GITHUB_REF_NAME"); v != "" {
			run.Branch = v
		}
	}
	run.PR = parsePRFromEnv()

	dirty, dirtyHash := workingTreeStatus(opts.RepoDir)
	run.Dirty = dirty
	run.DirtyHash = dirtyHash

	return run, nil
}

// gitBackend stores runs/entries on a dedicated git data branch. Default
// remote is the user's `origin`; with NoRemote the branch lives only in
// the user's local .git directory.
//
// Concurrent writers are reconciled by retrying push up to
// maxPushAttempts times. On a non-fast-forward rejection the backend
// fetches the current tip and rebases — git's three-way merge runs the
// `merge=union` driver from .gitattributes. RunRecord files have unique
// filenames per run_id and never conflict.
type gitBackend struct {
	opts Options
}

// InitialisePersistence is a no-op: the git backend's storage is the
// data branch itself, which is created lazily on first write inside
// InsertNewTestResults (the workdir is per-call and ephemeral).
func (b *gitBackend) InitialisePersistence() error { return nil }

func (b *gitBackend) InsertNewTestResults(run RunRecord, results []models.TestResult) error {
	if len(results) == 0 {
		return nil
	}
	if run.RunID == "" {
		return errors.New("persist: empty RunID")
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

	if err := writeRunRecord(workDir, run); err != nil {
		return err
	}
	if err := appendEntries(workDir, run, results); err != nil {
		return err
	}

	if err := commitAll(workDir, commitMessage(run, len(results))); err != nil {
		return err
	}

	return pushWithRetry(workDir, branch, branchExisted)
}

func (b *gitBackend) GetTestHistory(testName string) ([]HistoricalEntry, error) {
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
	// MkdirTemp created it; clone wants an empty path.
	_ = os.Remove(workDir)

	if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
		return nil, fmt.Errorf("clone data branch: %w", err)
	}

	return readHistoryFromDir(workDir, testName)
}

// fileBackend writes runs and entries to a plain directory on disk and
// performs no git operations. The directory is reused across runs so the
// developer can inspect accumulated output; it is the user's
// responsibility to gitignore it.
type fileBackend struct {
	dir string
}

func (b *fileBackend) InitialisePersistence() error {
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", b.dir, err)
	}
	return nil
}

func (b *fileBackend) InsertNewTestResults(run RunRecord, results []models.TestResult) error {
	if len(results) == 0 {
		return nil
	}
	if run.RunID == "" {
		return errors.New("persist: empty RunID")
	}
	if err := b.InitialisePersistence(); err != nil {
		return err
	}
	if err := writeRunRecord(b.dir, run); err != nil {
		return err
	}
	return appendEntries(b.dir, run, results)
}

func (b *fileBackend) GetTestHistory(testName string) ([]HistoricalEntry, error) {
	if _, err := os.Stat(b.dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return readHistoryFromDir(b.dir, testName)
}

// readHistoryFromDir reads tests/<id>.ndjson from a directory laid out
// like the data branch and joins each entry with its RunRecord from
// runs/<run_id>.json. Shared by gitBackend (after cloning) and
// fileBackend.
func readHistoryFromDir(dir, testName string) ([]HistoricalEntry, error) {
	path := filepath.Join(dir, "tests", EncodeTestID(testName)+".ndjson")
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	entries, err := parseNDJSON(f)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Timestamp < entries[j].Timestamp })

	out := make([]HistoricalEntry, 0, len(entries))
	runCache := map[string]RunRecord{}
	for _, e := range entries {
		rec, ok := runCache[e.RunID]
		if !ok {
			rec, _ = readRunRecord(dir, e.RunID)
			runCache[e.RunID] = rec
		}
		out = append(out, HistoricalEntry{Test: e, Run: rec})
	}
	return out, nil
}

// --- internal helpers ---

// gitErr carries the captured stderr and exit code so callers can branch
// on specific failure modes (most importantly: "ref does not exist", a.k.a.
// exit code 2 from ls-remote / remote get-url / similar).
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

// runGit invokes `git` with the given args. If dir is non-empty it becomes
// the child's working directory; the empty string means "inherit cwd",
// fine for any URL-driven command (clone, ls-remote).
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
		return strings.TrimSpace(stdout.String()), &gitErr{
			args:   args,
			err:    err,
			stderr: strings.TrimSpace(stderr.String()),
			code:   code,
		}
	}
	return strings.TrimSpace(stdout.String()), nil
}

// resolveTargetURL returns the URL to clone/push the data branch from. In
// remote mode (default) it's the user's origin; in local mode it's the
// user's own .git directory, so the data branch lives in their repo.
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

// localGitDir returns the absolute path of the user's .git directory,
// resolving worktree gitlinks via the git CLI so it works inside any
// checkout.
func localGitDir(repoDir string) (string, error) {
	out, err := runGit(repoDir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(repoDir, out)
	}
	// Must be absolute: this path is used as the origin URL for an
	// ephemeral workdir elsewhere on disk, so a relative path would
	// resolve against the wrong cwd and the push would silently no-op.
	return filepath.Abs(out)
}

// branchExistsOnRemote returns true iff refs/heads/<branch> is published
// at the remote. Uses ls-remote, which exits 2 when no ref matches.
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

// openOrInitDataRepo prepares workDir as a checkout of the data branch.
// If the branch already exists on the remote it is shallow-cloned;
// otherwise workDir is initialised as a fresh repo with HEAD pointed at
// the new branch (no parent history, ready for the first commit).
func openOrInitDataRepo(workDir, remoteURL, branch string) (branchExisted bool, err error) {
	exists, err := branchExistsOnRemote(remoteURL, branch)
	if err != nil {
		return false, err
	}
	if exists {
		// `git clone <url> <dest>` requires dest to be empty or missing.
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

func writeSeed(workDir string) error {
	if err := os.WriteFile(filepath.Join(workDir, ".gitattributes"), []byte(gitAttributes), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(workDir, "README.md"), []byte(readme), 0o644)
}

func writeRunRecord(workDir string, run RunRecord) error {
	dir := filepath.Join(workDir, "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal run record: %w", err)
	}
	path := filepath.Join(dir, run.RunID+".json")
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func readRunRecord(workDir, runID string) (RunRecord, error) {
	path := filepath.Join(workDir, "runs", runID+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return RunRecord{}, err
	}
	var run RunRecord
	if err := json.Unmarshal(b, &run); err != nil {
		return RunRecord{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return run, nil
}

func appendEntries(workDir string, run RunRecord, results []models.TestResult) error {
	dir := filepath.Join(workDir, "tests")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, r := range results {
		entry := toEntry(r, run)
		line, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshal entry for %s: %w", r.Id, err)
		}
		path := filepath.Join(dir, entry.TestID+".ndjson")
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
	}
	return nil
}

func toEntry(r models.TestResult, run RunRecord) Entry {
	ts := r.StartTime
	if ts.IsZero() {
		ts = time.Now()
	}
	return Entry{
		Schema:     SchemaVersion,
		TestID:     EncodeTestID(r.Id),
		TestName:   r.Id,
		RunID:      run.RunID,
		Timestamp:  ts.UTC().Format(time.RFC3339Nano),
		Ran:        r.Ran,
		Passed:     r.Passed,
		Status:     deriveStatus(r),
		DurationMs: r.Duration.Milliseconds(),
		Output:     r.Output,
	}
}

// deriveStatus collapses go test outcomes into a small enum useful for
// flaky-test classification: "skip", "pass", "panic", or "fail".
func deriveStatus(r models.TestResult) string {
	if !r.Ran {
		return "skip"
	}
	if r.Passed {
		return "pass"
	}
	if strings.Contains(r.Output, "panic:") {
		return "panic"
	}
	return "fail"
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
		// Best-effort cleanup so the workdir is in a known state.
		_, _ = runGit(workDir, "rebase", "--abort")
		return fmt.Errorf("git rebase %s: %w", target, err)
	}
	return nil
}

func commitMessage(run RunRecord, n int) string {
	short := run.Commit
	if len(short) > 7 {
		short = short[:7]
	}
	if short == "" {
		short = run.RunID
		if len(short) > 8 {
			short = short[:8]
		}
	}
	return fmt.Sprintf("results for %s (%d entries)", short, n)
}

func parseNDJSON(r io.Reader) ([]Entry, error) {
	out := []Entry{}
	sc := bufio.NewScanner(r)
	// Allow large lines: long Output captures can exceed the default 64KB.
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("parse ndjson line: %w", err)
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

func cmdHash(cmd []string) string {
	if len(cmd) == 0 {
		return ""
	}
	h := sha1.Sum([]byte(strings.Join(cmd, "\x00")))
	return hex.EncodeToString(h[:8])
}

// workingTreeStatus reports whether the user's repo has uncommitted
// changes. When dirty, the second return value is a stable hash of the
// unified diff against HEAD so two distinct dirty states do not look like
// the same run. Falls back to a hash of the status text if `git diff`
// can't be run for any reason.
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
	// On GitHub Actions PR runs, GITHUB_REF is "refs/pull/<n>/merge"
	// or "refs/pull/<n>/head".
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
