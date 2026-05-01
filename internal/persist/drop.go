package persist

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DropSelector chooses which signal directories to drop. At least one
// flag must be true; both true means "drop everything".
//
// BeforeUTC, when non-zero, scopes the drop to runs whose YYYY/MM/DD
// partition is strictly before the cutoff. Files dated >= the cutoff
// are kept. Zero value drops every file matching the selector.
type DropSelector struct {
	DropTraces  bool
	DropMetrics bool
	BeforeUTC   time.Time
}

// DropPlan is the inventory shown to the user before a drop is executed.
// All counts and bytes are computed from the data branch (or scratch dir)
// as it currently exists; the date range is derived from the YYYY/MM/DD
// path partitioning under traces/ and metrics/.
type DropPlan struct {
	Branch        string
	OriginURL     string // empty in dev mode
	Dev           bool
	TraceFiles    int
	MetricFiles   int
	TraceBytes    int64
	MetricBytes   int64
	OldestRunUTC  time.Time
	NewestRunUTC  time.Time
	SuppressionsN int
	BranchMissing bool // true when origin has no data branch yet
	Sel           DropSelector
}

// Nothing reports whether the plan describes a no-op: either the branch
// is missing, or the chosen selectors match zero files.
func (p DropPlan) Nothing() bool {
	if p.BranchMissing {
		return true
	}
	if p.Sel.DropTraces && p.TraceFiles > 0 {
		return false
	}
	if p.Sel.DropMetrics && p.MetricFiles > 0 {
		return false
	}
	return true
}

// DropHistory inventories the data branch (or dev scratch dir) and, if
// confirm returns true, rewrites it. For the git backend the rewrite is
// a single orphan commit force-pushed with --force-with-lease against the
// tip we cloned, so a concurrent writer can't be silently clobbered. For
// the dev backend the chosen signal directories are simply deleted.
//
// confirm is called exactly once after the inventory is built. Returning
// false aborts cleanly with no modifications. confirm is also called when
// the plan is a no-op so the CLI can print "nothing to drop" with the
// same context (branch / scratch dir).
func (b *gitBackend) DropHistory(sel DropSelector, confirm func(DropPlan) bool) error {
	if !sel.DropTraces && !sel.DropMetrics {
		return errors.New("drop: nothing selected (need DropTraces and/or DropMetrics)")
	}

	branch := b.dataBranch()
	remoteURL, err := resolveTargetURL(b.opts)
	if err != nil {
		return err
	}

	exists, err := branchExistsOnRemote(remoteURL, branch)
	if err != nil {
		return err
	}
	if !exists {
		plan := DropPlan{Branch: branch, OriginURL: remoteURL, BranchMissing: true, Sel: sel}
		if confirm != nil {
			confirm(plan)
		}
		return nil
	}

	workDir, err := os.MkdirTemp("", "defrost-drop-")
	if err != nil {
		return fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(workDir)
	_ = os.Remove(workDir) // git clone wants the path absent

	if _, err := runGit("", "clone", "--quiet", "--depth=1", "--single-branch", "--branch", branch, remoteURL, workDir); err != nil {
		return fmt.Errorf("clone data branch: %w", err)
	}
	clonedTip, err := runGit(workDir, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("rev-parse HEAD: %w", err)
	}

	plan := buildDropPlan(workDir, sel)
	plan.Branch = branch
	plan.OriginURL = remoteURL

	if plan.Nothing() {
		if confirm != nil {
			confirm(plan)
		}
		return nil
	}
	if confirm != nil && !confirm(plan) {
		return nil
	}

	if err := configureBotIdentity(workDir); err != nil {
		return err
	}
	if err := dropSignalFiles(workDir, sel); err != nil {
		return err
	}

	// Build a single orphan commit on a fresh branch, then force-push it
	// onto <branch> with a lease against the SHA we cloned. The lease
	// makes a concurrent writer's push between our clone and our push
	// fail loudly here instead of silently destroying their run.
	const tmpBranch = "defrost-drop-tmp"
	if _, err := runGit(workDir, "checkout", "--orphan", tmpBranch); err != nil {
		return fmt.Errorf("checkout orphan: %w", err)
	}
	if _, err := runGit(workDir, "add", "-A", "."); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if _, err := runGit(workDir, "commit", "--quiet", "-m", dropCommitMessage(sel)); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	// No leading '+' on the refspec: '+' (and bare --force) bypass
	// --force-with-lease, which would defeat the whole point of capturing
	// the cloned tip. The lease alone provides the force-when-safe gate.
	refspec := fmt.Sprintf("refs/heads/%s:refs/heads/%s", tmpBranch, branch)
	leasespec := fmt.Sprintf("refs/heads/%s:%s", branch, clonedTip)
	if _, err := runGit(workDir, "push", "--quiet", "--force-with-lease="+leasespec, "origin", refspec); err != nil {
		return fmt.Errorf("force-push: %w", err)
	}
	return nil
}

func (b *fileBackend) DropHistory(sel DropSelector, confirm func(DropPlan) bool) error {
	if !sel.DropTraces && !sel.DropMetrics {
		return errors.New("drop: nothing selected (need DropTraces and/or DropMetrics)")
	}

	plan := buildDropPlan(b.dir, sel)
	plan.Dev = true
	if plan.Nothing() {
		if confirm != nil {
			confirm(plan)
		}
		return nil
	}
	if confirm != nil && !confirm(plan) {
		return nil
	}

	return dropSignalFiles(b.dir, sel)
}

func buildDropPlan(dir string, sel DropSelector) DropPlan {
	plan := DropPlan{Sel: sel}
	// The cutoff only narrows files for signals we're actually dropping.
	// A "preserved" signal in a mixed-scope drop reports its TOTAL file
	// count regardless of cutoff — otherwise the UI's
	// "preserved · N files, X KiB" line underreports because files
	// on/after the cutoff would be excluded.
	var traceCutoff, metricCutoff time.Time
	if sel.DropTraces {
		traceCutoff = sel.BeforeUTC
	}
	if sel.DropMetrics {
		metricCutoff = sel.BeforeUTC
	}
	plan.TraceFiles, plan.TraceBytes = inventorySignalDir(filepath.Join(dir, "traces"), traceCutoff)
	plan.MetricFiles, plan.MetricBytes = inventorySignalDir(filepath.Join(dir, "metrics"), metricCutoff)
	plan.OldestRunUTC, plan.NewestRunUTC = dateRangeFromPartitions(dir)
	if ids, err := readSuppressionsFile(dir); err == nil {
		plan.SuppressionsN = len(ids)
	}
	return plan
}

// dropSignalFiles applies sel against the working tree at root: when
// sel.BeforeUTC is zero each chosen signal directory is removed
// wholesale; when set only files dated strictly before the cutoff are
// removed and any directories that became empty are pruned.
func dropSignalFiles(root string, sel DropSelector) error {
	tracesDir := filepath.Join(root, "traces")
	metricsDir := filepath.Join(root, "metrics")
	if sel.BeforeUTC.IsZero() {
		if sel.DropTraces {
			if err := os.RemoveAll(tracesDir); err != nil {
				return fmt.Errorf("remove traces: %w", err)
			}
		}
		if sel.DropMetrics {
			if err := os.RemoveAll(metricsDir); err != nil {
				return fmt.Errorf("remove metrics: %w", err)
			}
		}
		return nil
	}
	if sel.DropTraces {
		if err := removeSignalFilesBefore(tracesDir, sel.BeforeUTC); err != nil {
			return fmt.Errorf("remove traces before %s: %w", sel.BeforeUTC.Format("2006-01-02"), err)
		}
	}
	if sel.DropMetrics {
		if err := removeSignalFilesBefore(metricsDir, sel.BeforeUTC); err != nil {
			return fmt.Errorf("remove metrics before %s: %w", sel.BeforeUTC.Format("2006-01-02"), err)
		}
	}
	return nil
}

// inventorySignalDir reports the file count and total bytes under dir
// that *would be dropped*. If beforeUTC is zero every signal file
// counts; otherwise only files whose YYYY/MM/DD path partition is
// strictly before the cutoff count.
func inventorySignalDir(dir string, beforeUTC time.Time) (int, int64) {
	var n int
	var bytes int64
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), fileSuffix) {
			return nil
		}
		if !beforeUTC.IsZero() {
			rel, rerr := filepath.Rel(dir, path)
			if rerr != nil {
				return nil
			}
			t, ok := partitionDateFromRel(rel)
			if !ok || !t.Before(beforeUTC) {
				return nil
			}
		}
		n++
		if info, ierr := d.Info(); ierr == nil {
			bytes += info.Size()
		}
		return nil
	})
	return n, bytes
}

// partitionDateFromRel parses YYYY/MM/DD/<id>.otlp.pb.zst into a UTC
// midnight timestamp. Returns ok=false for any path that doesn't match
// the partitioning scheme — those files are conservatively kept by the
// before-cutoff filter.
func partitionDateFromRel(rel string) (time.Time, bool) {
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 4 {
		return time.Time{}, false
	}
	y, e1 := strconv.Atoi(parts[0])
	m, e2 := strconv.Atoi(parts[1])
	day, e3 := strconv.Atoi(parts[2])
	if e1 != nil || e2 != nil || e3 != nil {
		return time.Time{}, false
	}
	return time.Date(y, time.Month(m), day, 0, 0, 0, 0, time.UTC), true
}

// removeSignalFilesBefore deletes only files under dir whose YYYY/MM/DD
// partition is strictly before beforeUTC, then prunes any directories
// that became empty. Used by DropHistory when sel.BeforeUTC is set so
// the partial-drop case still produces a clean tree to commit.
func removeSignalFilesBefore(dir string, beforeUTC time.Time) error {
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), fileSuffix) {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return nil
		}
		t, ok := partitionDateFromRel(rel)
		if !ok || !t.Before(beforeUTC) {
			return nil
		}
		return os.Remove(path)
	})
	if err != nil {
		return err
	}
	return pruneEmptyDirs(dir)
}

// pruneEmptyDirs removes every empty directory under root, leaving root
// itself in place even if it ends up empty.
func pruneEmptyDirs(root string) error {
	var dirs []string
	if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || !d.IsDir() {
			return nil
		}
		dirs = append(dirs, p)
		return nil
	}); err != nil {
		return err
	}
	// Walk parents-last so children get a chance to be removed first.
	for i := len(dirs) - 1; i >= 0; i-- {
		if dirs[i] == root {
			continue
		}
		entries, err := os.ReadDir(dirs[i])
		if err == nil && len(entries) == 0 {
			_ = os.Remove(dirs[i])
		}
	}
	return nil
}

func dateRangeFromPartitions(workDir string) (oldest, newest time.Time) {
	for _, signal := range []string{"traces", "metrics"} {
		root := filepath.Join(workDir, signal)
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d == nil {
				return nil
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), fileSuffix) {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return nil
			}
			parts := strings.Split(rel, string(filepath.Separator))
			if len(parts) < 4 {
				return nil
			}
			y, e1 := strconv.Atoi(parts[0])
			m, e2 := strconv.Atoi(parts[1])
			day, e3 := strconv.Atoi(parts[2])
			if e1 != nil || e2 != nil || e3 != nil {
				return nil
			}
			t := time.Date(y, time.Month(m), day, 0, 0, 0, 0, time.UTC)
			if oldest.IsZero() || t.Before(oldest) {
				oldest = t
			}
			if newest.IsZero() || t.After(newest) {
				newest = t
			}
			return nil
		})
	}
	return
}

func dropCommitMessage(sel DropSelector) string {
	switch {
	case sel.DropTraces && sel.DropMetrics:
		return "defrost: drop history (traces + metrics)"
	case sel.DropTraces:
		return "defrost: drop history (traces)"
	case sel.DropMetrics:
		return "defrost: drop history (metrics)"
	}
	return "defrost: drop history"
}
