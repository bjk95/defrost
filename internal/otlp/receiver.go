package otlp

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configgrpc"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

// Receiver wraps the upstream `otlpreceiver` factory so defrost can use
// it as a library — without spinning up the full Collector service. The
// receiver listens on a random localhost HTTP port for /v1/traces,
// /v1/metrics, and /v1/logs and forwards every payload into the
// supplied Sink.
type Receiver struct {
	port    int
	traces  receiver.Traces
	metrics receiver.Metrics
	logs    receiver.Logs
}

// Start binds the receiver on 127.0.0.1 with a free port chosen by the
// kernel and returns the chosen port. All three signals are wired up to
// sink. gRPC is disabled — defrost only uses HTTP.
func Start(ctx context.Context, sink *Sink) (*Receiver, int, error) {
	port, err := pickFreePort()
	if err != nil {
		return nil, 0, fmt.Errorf("otlp receiver: pick port: %w", err)
	}
	factory := otlpreceiver.NewFactory()
	cfg := factory.CreateDefaultConfig().(*otlpreceiver.Config)
	// Default() optionals don't materialize a value until an unmarshal
	// or GetOrInsertDefault() promotes them. Promote HTTP so we can set
	// the listen endpoint, then disable gRPC entirely (defrost is
	// HTTP-only — gRPC support is one config flip away if a real bug
	// ever lands).
	httpCfg := cfg.Protocols.HTTP.GetOrInsertDefault()
	httpCfg.ServerConfig.NetAddr.Endpoint = "127.0.0.1:" + strconv.Itoa(port)
	cfg.Protocols.GRPC = configoptional.None[configgrpc.ServerConfig]()

	settings := receivertest.NewNopSettings(factory.Type())
	host := componenttest.NewNopHost()

	tr, err := factory.CreateTraces(ctx, settings, cfg, sink)
	if err != nil {
		return nil, 0, fmt.Errorf("otlp receiver: create traces: %w", err)
	}
	mr, err := factory.CreateMetrics(ctx, settings, cfg, sink)
	if err != nil {
		return nil, 0, fmt.Errorf("otlp receiver: create metrics: %w", err)
	}
	lr, err := factory.CreateLogs(ctx, settings, cfg, sink)
	if err != nil {
		return nil, 0, fmt.Errorf("otlp receiver: create logs: %w", err)
	}
	r := &Receiver{port: port, traces: tr, metrics: mr, logs: lr}

	// The traces, metrics, and logs receivers share the same underlying
	// otlpReceiver (sharedcomponent.LoadOrStore on the cfg pointer), so
	// a single Start call brings up the HTTP server. The other Start
	// calls are no-ops.
	if err := tr.Start(ctx, host); err != nil {
		return nil, 0, fmt.Errorf("otlp receiver: start traces: %w", err)
	}
	return r, port, nil
}

// Port returns the bound port.
func (r *Receiver) Port() int { return r.port }

// Shutdown stops the underlying receiver. The sink retains whatever
// pdata it accumulated — Drain it separately.
func (r *Receiver) Shutdown(ctx context.Context) error {
	if r == nil || r.traces == nil {
		return nil
	}
	return r.traces.Shutdown(ctx)
}

// pickFreePort opens an ephemeral listener, reads its assigned port,
// and closes it. The TIME_WAIT race window between close and the
// receiver's bind is small enough to ignore for a developer-machine
// CLI. If the race ever bites, the receiver Start fails loudly and the
// run continues without metric collection (see exec.go).
func pickFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}
