package serve

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// ProgressEvent is one tick of the loading-screen feed. Phase is one of
// the canonical IDs (connect/clone/spans/parse/metrics/index/ready) and
// marks a boundary; Stream carries an arbitrary log line for the current
// phase (e.g. "found 50 run files").
type ProgressEvent struct {
	Phase  string `json:"phase,omitempty"`
	Detail string `json:"detail,omitempty"`
	Stream string `json:"stream,omitempty"`
}

// ProgressEmitter is the callback shape Load uses to report phase
// boundaries. Default no-op when no SSE subscribers are watching.
type ProgressEmitter func(ProgressEvent)

// progressBus broadcasts events to any number of subscribers and retains
// the current boot's history so late subscribers replay it.
//
// One bus per server. /api/tests writes through the emitter (which fans
// to the bus); /api/loading/progress subscribes and streams to the
// browser. Reset() clears history at the start of each fresh load.
type progressBus struct {
	mu      sync.Mutex
	history []ProgressEvent
	subs    map[chan ProgressEvent]struct{}
}

func newProgressBus() *progressBus {
	return &progressBus{subs: make(map[chan ProgressEvent]struct{})}
}

func (p *progressBus) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.history = nil
}

func (p *progressBus) Emit(ev ProgressEvent) {
	p.mu.Lock()
	p.history = append(p.history, ev)
	subs := make([]chan ProgressEvent, 0, len(p.subs))
	for ch := range p.subs {
		subs = append(subs, ch)
	}
	p.mu.Unlock()
	for _, ch := range subs {
		// Non-blocking: a slow subscriber drops events, never blocks
		// the loader.
		select {
		case ch <- ev:
		default:
		}
	}
}

// Subscribe returns a channel of new events plus a snapshot of the
// current history (so late subscribers can replay). The returned cancel
// fn unregisters the channel and closes it.
func (p *progressBus) Subscribe() (<-chan ProgressEvent, []ProgressEvent, func()) {
	ch := make(chan ProgressEvent, 32)
	p.mu.Lock()
	p.subs[ch] = struct{}{}
	snapshot := append([]ProgressEvent(nil), p.history...)
	p.mu.Unlock()
	cancel := func() {
		p.mu.Lock()
		if _, ok := p.subs[ch]; ok {
			delete(p.subs, ch)
			close(ch)
		}
		p.mu.Unlock()
	}
	return ch, snapshot, cancel
}

// loadingProgressHandler returns an http.HandlerFunc that streams
// ProgressEvents from bus to the client over Server-Sent Events. The
// handler replays bus history on connect (so a tab subscribing mid-load
// catches up to the live state) then streams new events until the client
// disconnects. A 25-second heartbeat keeps proxies from timing out idle
// connections.
func loadingProgressHandler(bus *progressBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

		ch, snapshot, cancel := bus.Subscribe()
		defer cancel()

		for _, ev := range snapshot {
			if !writeSSE(w, flusher, ev) {
				return
			}
		}

		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if !writeSSE(w, flusher, ev) {
					return
				}
			case <-ticker.C:
				if _, err := w.Write([]byte(": ping\n\n")); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, ev ProgressEvent) bool {
	payload, err := json.Marshal(ev)
	if err != nil {
		return false
	}
	if _, err := w.Write([]byte("data: ")); err != nil {
		return false
	}
	if _, err := w.Write(payload); err != nil {
		return false
	}
	if _, err := w.Write([]byte("\n\n")); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
