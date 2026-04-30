package golang

import (
	"reflect"
	"testing"
)

func TestEnsureJSONFlagInsertsWhenMissing(t *testing.T) {
	in := []string{"go", "test", "-race", "./..."}
	want := []string{"go", "test", "-json", "-race", "./..."}
	got := ensureJSONFlag(in)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEnsureJSONFlagNoopWhenPresent(t *testing.T) {
	cases := [][]string{
		{"go", "test", "-json", "./..."},
		{"go", "test", "-race", "-json", "./..."},
		{"go", "test", "--json", "./..."},
		{"go", "test", "-json=true", "./..."},
		{"go", "test", "--json=true", "./..."},
	}
	for _, in := range cases {
		got := ensureJSONFlag(in)
		if !reflect.DeepEqual(got, in) {
			t.Errorf("ensureJSONFlag(%v) = %v, want unchanged", in, got)
		}
	}
}
