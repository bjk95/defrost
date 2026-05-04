package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/bjk95/defrost/internal/persist"
)

// HandleHistory implements `defrost history`. It reads every persisted
// trace file from the data branch (or scratch dir), decodes them, and
// emits one OTLP/JSON line per matching test span — sorted oldest
// first by start time.
//
// Reads raw bytes from the data branch on demand instead of going
// through the DuckDB Querier so the slim `defrost-ci` binary (which
// doesn't link DuckDB) can still serve `history`.
func HandleHistory(c HistoryCmd) int {
	pOpts := persist.Options{
		RepoDir:    c.RepoDir,
		DataBranch: c.DataBranch,
		AuthToken:  os.Getenv("GITHUB_TOKEN"),
		Dev:        c.Dev,
	}
	be := persist.New(pOpts)
	snap, err := be.CloneForRead()
	if err != nil {
		if errors.Is(err, persist.ErrNoOrigin) {
			fmt.Fprintln(os.Stderr, "history: no 'origin' remote configured. Either add one (`git remote add origin ...`) or pass --dev to read from the local scratch dir.")
			return 1
		}
		fmt.Fprintln(os.Stderr, "history:", err)
		return 1
	}
	if snap.Dir == "" {
		return 0
	}
	files, err := persist.ListSignalFiles(snap.Dir, "traces")
	if err != nil {
		fmt.Fprintln(os.Stderr, "history:", err)
		return 1
	}
	type entry struct {
		startNs uint64
		json    string
	}
	var entries []entry
	for _, path := range files {
		raw, err := persist.ReadSignalBytes(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "history: read", path, ":", err)
			continue
		}
		req := ptraceotlp.NewExportRequest()
		if err := req.UnmarshalProto(raw); err != nil {
			fmt.Fprintln(os.Stderr, "history: decode", path, ":", err)
			continue
		}
		td := req.Traces()
		rs := td.ResourceSpans()
		for i := 0; i < rs.Len(); i++ {
			r := rs.At(i)
			ss := r.ScopeSpans()
			for j := 0; j < ss.Len(); j++ {
				spans := ss.At(j).Spans()
				for k := 0; k < spans.Len(); k++ {
					s := spans.At(k)
					if s.Name() != c.Test {
						continue
					}
					line, err := encodeSingleSpan(r, ss.At(j), s)
					if err != nil {
						fmt.Fprintln(os.Stderr, "history: marshal:", err)
						continue
					}
					entries = append(entries, entry{startNs: uint64(s.StartTimestamp()), json: line})
				}
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].startNs < entries[j].startNs })
	for _, e := range entries {
		fmt.Println(e.json)
	}
	return 0
}

// encodeSingleSpan builds a fresh ptrace.Traces holding exactly one
// (Resource, Scope, Span) triple — the original surrounding context
// preserved verbatim — and serializes it as OTLP/JSON. NDJSON-friendly:
// embedded newlines are stripped from the output.
func encodeSingleSpan(rs ptrace.ResourceSpans, ss ptrace.ScopeSpans, s ptrace.Span) (string, error) {
	td := ptrace.NewTraces()
	out := td.ResourceSpans().AppendEmpty()
	rs.Resource().CopyTo(out.Resource())
	scope := out.ScopeSpans().AppendEmpty()
	ss.Scope().CopyTo(scope.Scope())
	dst := scope.Spans().AppendEmpty()
	s.CopyTo(dst)
	b, err := ptraceotlp.NewExportRequestFromTraces(td).MarshalJSON()
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(b), "\n", " "), nil
}
