package main

import (
	"testing"

	"github.com/bjk95/defrost/internal/persist"
)

func TestHandleSuppressAdd_Single(t *testing.T) {
	repo := makeRepo(t)
	opts := SuppressOpts{RepoDir: repo, NoRemote: true, Dev: true}

	code := HandleSuppressAdd([]string{"pkg/TestA"}, opts)
	if code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}

	be := persist.New(opts.toPersist())
	got, err := be.GetSuppressions()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 || got[0] != "pkg/TestA" {
		t.Fatalf("expected [pkg/TestA], got %v", got)
	}
}

func TestHandleSuppressAdd_MultipleIDs(t *testing.T) {
	repo := makeRepo(t)
	opts := SuppressOpts{RepoDir: repo, NoRemote: true, Dev: true}

	code := HandleSuppressAdd([]string{"a", "b", "c"}, opts)
	if code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}

	be := persist.New(opts.toPersist())
	got, err := be.GetSuppressions()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 ids, got %v", got)
	}
}

func TestHandleSuppressAdd_EmptySliceFails(t *testing.T) {
	repo := makeRepo(t)
	opts := SuppressOpts{RepoDir: repo, NoRemote: true, Dev: true}
	code := HandleSuppressAdd(nil, opts)
	if code != 2 {
		t.Fatalf("expected 2, got %d", code)
	}
}

func TestHandleSuppressAdd_SkipsEmptyStrings(t *testing.T) {
	repo := makeRepo(t)
	opts := SuppressOpts{RepoDir: repo, NoRemote: true, Dev: true}
	code := HandleSuppressAdd([]string{"", "real-id", ""}, opts)
	if code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
	be := persist.New(opts.toPersist())
	got, err := be.GetSuppressions()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 || got[0] != "real-id" {
		t.Fatalf("expected [real-id], got %v", got)
	}
}

func TestHandleSuppressAdd_AllEmptyStringsFails(t *testing.T) {
	repo := makeRepo(t)
	opts := SuppressOpts{RepoDir: repo, NoRemote: true, Dev: true}
	code := HandleSuppressAdd([]string{"", ""}, opts)
	if code != 2 {
		t.Fatalf("expected 2, got %d", code)
	}
}

func TestCommitMessageForAdd_Single(t *testing.T) {
	got := commitMessageForAdd([]string{"pkg/TestFoo"})
	want := "suppress: add pkg/TestFoo"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestCommitMessageForAdd_Multiple(t *testing.T) {
	got := commitMessageForAdd([]string{"a", "b", "c"})
	want := "suppress: add 3 tests"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}
