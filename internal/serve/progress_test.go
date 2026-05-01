package serve

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProgressBus_LateSubscriberReceivesHistory(t *testing.T) {
	bus := newProgressBus()
	bus.Emit(ProgressEvent{Phase: "connect"})
	bus.Emit(ProgressEvent{Phase: "clone"})

	_, snapshot, cancel := bus.Subscribe()
	defer cancel()

	if len(snapshot) != 2 || snapshot[0].Phase != "connect" || snapshot[1].Phase != "clone" {
		t.Fatalf("late subscriber should replay history, got %+v", snapshot)
	}
}

func TestProgressBus_ResetClearsHistory(t *testing.T) {
	bus := newProgressBus()
	bus.Emit(ProgressEvent{Phase: "clone"})
	bus.Reset()

	_, snapshot, cancel := bus.Subscribe()
	defer cancel()
	if len(snapshot) != 0 {
		t.Fatalf("Reset must clear history, got %+v", snapshot)
	}
}

func TestLoadingProgressHandler_StreamsHistoryAndLiveEvents(t *testing.T) {
	bus := newProgressBus()
	bus.Emit(ProgressEvent{Phase: "connect", Detail: "git ls-remote"})

	srv := httptest.NewServer(loadingProgressHandler(bus))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type: want text/event-stream, got %q", got)
	}

	// Push a live event after the connection is established.
	go func() {
		time.Sleep(50 * time.Millisecond)
		bus.Emit(ProgressEvent{Phase: "clone", Detail: "git clone"})
	}()

	br := bufio.NewReader(resp.Body)
	gotConnect, gotClone := false, false
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !(gotConnect && gotClone) {
		line, err := br.ReadString('\n')
		if err != nil {
			break
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		switch {
		case strings.Contains(line, `"phase":"connect"`):
			gotConnect = true
		case strings.Contains(line, `"phase":"clone"`):
			gotClone = true
		}
	}
	if !gotConnect {
		t.Errorf("did not receive replayed connect event")
	}
	if !gotClone {
		t.Errorf("did not receive live clone event")
	}
}
