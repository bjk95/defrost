package persist

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestReadSuppressionsFile_AbsentReturnsEmpty(t *testing.T) {
	got, err := readSuppressionsFile(t.TempDir())
	if err != nil {
		t.Fatalf("readSuppressionsFile: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}
}

func TestWriteSuppressionsFile_SortsAndDedupes(t *testing.T) {
	dir := t.TempDir()
	if err := writeSuppressionsFile(dir, []string{"b", "a", "a", "c"}); err != nil {
		t.Fatalf("writeSuppressionsFile: %v", err)
	}

	got, err := readSuppressionsFile(dir)
	if err != nil {
		t.Fatalf("readSuppressionsFile: %v", err)
	}
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip: want %v got %v", want, got)
	}
}

func TestWriteSuppressionsFile_StableSortAcrossWrites(t *testing.T) {
	dir := t.TempDir()
	if err := writeSuppressionsFile(dir, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "suppressions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSuppressionsFile(dir, []string{"b", "a"}); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "suppressions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("write order should not affect bytes:\n first:  %s\n second: %s", first, second)
	}
}

func TestReadSuppressionsFile_MalformedReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "suppressions.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSuppressionsFile(dir); err == nil {
		t.Errorf("expected error reading malformed file, got nil")
	}
}

func TestSortAndDedupe(t *testing.T) {
	got := sortAndDedupe([]string{"b", "a", "b", "c", "a"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v got %v", want, got)
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("not sorted: %v", got)
	}
}
