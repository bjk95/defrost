package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/bjk95/defrost/internal/persist"
)

// fakeSuppressionsBackend is a thread-safe in-memory stand-in for
// persist.Backend used to exercise the HTTP suppression endpoints
// without a git working tree.
type fakeSuppressionsBackend struct {
	mu  sync.Mutex
	ids []string
}

func (f *fakeSuppressionsBackend) GetSuppressions() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.ids...)
	sort.Strings(out)
	return out, nil
}

func (f *fakeSuppressionsBackend) UpdateSuppressions(mutate func([]string) []string, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := mutate(append([]string(nil), f.ids...))
	// dedupe + sort to mirror persist.writeSuppressionsFile
	seen := make(map[string]struct{}, len(next))
	out := make([]string, 0, len(next))
	for _, id := range next {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	f.ids = out
	return nil
}

func newSuppressionsServer(t *testing.T, initial []string) (*httptest.Server, *fakeSuppressionsBackend) {
	t.Helper()
	fake := &fakeSuppressionsBackend{ids: append([]string(nil), initial...)}
	prev := suppressionsBackendFn
	suppressionsBackendFn = func(_ persist.Options) suppressionsBackend { return fake }
	t.Cleanup(func() { suppressionsBackendFn = prev })

	prevLoader := loaderFn
	loaderFn = func(_ persist.Options, _ ProgressEmitter) (Dataset, error) { return Dataset{}, nil }
	t.Cleanup(func() { loaderFn = prevLoader })

	h := New(persist.Options{}, fstest.MapFS{})
	return httptest.NewServer(h), fake
}

func TestSuppressions_Get_ReturnsEncodedList(t *testing.T) {
	rawA := `examples/promptfoo¬answer="123 invalid"`
	rawB := "pkg.TestB"
	srv, _ := newSuppressionsServer(t, []string{rawA, rawB})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/suppressions")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	var body suppressionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []string{persist.EncodeName(rawA), persist.EncodeName(rawB)}
	if !equalStrings(body.TestIDs, want) {
		t.Errorf("TestIDs: want %v, got %v", want, body.TestIDs)
	}
}

func TestSuppressions_Post_DecodesAndStoresRaw(t *testing.T) {
	rawID := `examples/promptfoo¬answer="new"`
	srv, fake := newSuppressionsServer(t, nil)
	defer srv.Close()

	encoded := persist.EncodeName(rawID)
	body := bytes.NewBufferString(`{"test_id":` + jsonString(encoded) + `}`)
	resp, err := http.Post(srv.URL+"/api/suppressions", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status: want 201, got %d", resp.StatusCode)
	}
	var got suppressionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !equalStrings(got.TestIDs, []string{encoded}) {
		t.Errorf("response (encoded): want [%s], got %v", encoded, got.TestIDs)
	}
	if !equalStrings(fake.ids, []string{rawID}) {
		t.Errorf("backend (raw): want [%s], got %v", rawID, fake.ids)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestSuppressions_Post_RejectsEmptyTestID(t *testing.T) {
	srv, _ := newSuppressionsServer(t, nil)
	defer srv.Close()

	body := bytes.NewBufferString(`{"test_id":""}`)
	resp, err := http.Post(srv.URL+"/api/suppressions", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", resp.StatusCode)
	}
}

func TestSuppressions_Delete_RemovesAndReturnsEncodedList(t *testing.T) {
	rawTarget := `examples/inspect¬task="capital_cities",sample="empty-fail"`
	rawKeep := "pkg.Other"
	srv, fake := newSuppressionsServer(t, []string{rawTarget, rawKeep})
	defer srv.Close()

	// The URL path carries the encoded form (matches TestRow.test_id), then
	// PathEscape applied a second time as part of standard URL encoding.
	encodedTarget := persist.EncodeName(rawTarget)
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/suppressions/"+url.PathEscape(encodedTarget), nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	var got suppressionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	wantWire := []string{persist.EncodeName(rawKeep)}
	if !equalStrings(got.TestIDs, wantWire) {
		t.Errorf("response (encoded): want %v, got %v", wantWire, got.TestIDs)
	}
	if !equalStrings(fake.ids, []string{rawKeep}) {
		t.Errorf("backend (raw): want [%s], got %v", rawKeep, fake.ids)
	}
}

func TestSuppressions_MethodNotAllowed(t *testing.T) {
	srv, _ := newSuppressionsServer(t, nil)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/suppressions", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status: want 405, got %d", resp.StatusCode)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
