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
	r.port = ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/metrics", r.handleMetrics)
	r.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = r.server.Serve(ln) }()
	return r.port, nil
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
	out := r.buf
	r.buf = nil
	r.mu.Unlock()
	if server == nil {
		return out, nil
	}
	return out, server.Shutdown(ctx)
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
	body, err := io.ReadAll(req.Body)
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
