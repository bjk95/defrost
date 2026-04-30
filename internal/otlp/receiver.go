package otlp

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	cmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// Receiver is a minimal OTLP/HTTP listener for metrics. It binds a
// random localhost port, accepts POST /v1/metrics requests with
// Content-Type: application/x-protobuf, and buffers the decoded
// ExportMetricsServiceRequest messages in memory until Shutdown is called.
type Receiver struct {
	server *http.Server
	port   int
	mu     sync.Mutex
	buf    []*cmetricspb.ExportMetricsServiceRequest
	closed bool
}

// New returns a non-started Receiver.
func New() *Receiver { return &Receiver{} }

// Start binds 127.0.0.1 on a free port and serves until Shutdown.
// Returns the chosen port.
func (r *Receiver) Start() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("otlp receiver: bind: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/metrics", r.handleMetrics)
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	port := ln.Addr().(*net.TCPAddr).Port

	r.mu.Lock()
	r.port = port
	r.server = srv
	r.mu.Unlock()

	go func() { _ = srv.Serve(ln) }()
	return port, nil
}

// Shutdown stops accepting new connections, waits for in-flight handlers
// to drain bounded by ctx, and returns the buffered metric requests.
// Subsequent calls return (nil, nil).
func (r *Receiver) Shutdown(ctx context.Context) ([]*cmetricspb.ExportMetricsServiceRequest, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, nil
	}
	r.closed = true
	server := r.server
	r.mu.Unlock()

	// Drain in-flight handlers BEFORE capturing the buffer. server.Shutdown
	// blocks until every active handleMetrics call returns; once it does,
	// no new handler can run, so capturing r.buf afterwards is exhaustive.
	var err error
	if server != nil {
		err = server.Shutdown(ctx)
	}

	r.mu.Lock()
	out := r.buf
	r.buf = nil
	r.mu.Unlock()

	return out, err
}

func (r *Receiver) handleMetrics(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ct := req.Header.Get("Content-Type"); ct != "application/x-protobuf" {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	// Cap inbound payloads at 16 MiB. Defrost's expected workload is one
	// run's worth of OTLP exports — far below this — so the cap is a
	// guard against misbehaving clients, not a real ceiling.
	const maxBody = 16 << 20
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	msg := &cmetricspb.ExportMetricsServiceRequest{}
	if err := proto.Unmarshal(body, msg); err != nil {
		http.Error(w, "decode protobuf", http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	r.buf = append(r.buf, msg)
	r.mu.Unlock()
	resp, _ := proto.Marshal(&cmetricspb.ExportMetricsServiceResponse{})
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}
