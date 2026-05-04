// Shared OTel SDK bootstrap for the JS / TS examples (jest, vitest).
//
// Wires up traces, metrics, and logs against the OTLP/HTTP endpoint
// that `defrost exec` exports via OTEL_EXPORTER_OTLP_ENDPOINT. Standalone
// `npm test` runs unchanged — when the env var is absent, this file
// returns inert no-ops so importing it is free.
//
// Three providers (one per signal). Each is shut down explicitly on
// `beforeExit` so its batch processor flushes before the runner exits;
// defrost only waits 2s after the child for stragglers, and the OTel
// JS SDK's default batch interval can be longer than that.
//
// Usage from a test file (CommonJS):
//
//   const { tracer, meter, logger } = require('../_otel-setup');
//   if (tracer) {
//     const span = tracer.startSpan('my-custom-span');
//     meter.createCounter('example.invocations').add(1);
//     logger.emit({ severityText: 'INFO', body: 'hello-from-test' });
//     span.end();
//   }
//
// Usage from TS:
//
//   import { tracer, meter, logger } from '../_otel-setup';

'use strict';

function noopExports() {
  return {
    tracer: null,
    meter: null,
    logger: null,
    shutdown: () => Promise.resolve(),
  };
}

function realExports() {
  let api, traceSDK, metricsSDK, logsSDK, traceExp, metricExp, logExp;
  try {
    api = require('@opentelemetry/api');
    traceSDK = require('@opentelemetry/sdk-trace-base');
    metricsSDK = require('@opentelemetry/sdk-metrics');
    logsSDK = require('@opentelemetry/sdk-logs');
    traceExp = require('@opentelemetry/exporter-trace-otlp-http');
    metricExp = require('@opentelemetry/exporter-metrics-otlp-http');
    logExp = require('@opentelemetry/exporter-logs-otlp-http');
  } catch (err) {
    // OTel SDK packages aren't installed in this environment. The
    // CI integration jobs install them explicitly; outside CI this
    // is a benign no-op.
    return noopExports();
  }

  // --- Traces
  const tracerProvider = new traceSDK.BasicTracerProvider({
    spanProcessors: [
      new traceSDK.BatchSpanProcessor(new traceExp.OTLPTraceExporter()),
    ],
  });
  api.trace.setGlobalTracerProvider(tracerProvider);

  // --- Metrics
  const meterProvider = new metricsSDK.MeterProvider({
    readers: [
      new metricsSDK.PeriodicExportingMetricReader({
        exporter: new metricExp.OTLPMetricExporter(),
        // Tight interval so the reader's first export lands well
        // within defrost's 2s drain window even if shutdown()
        // doesn't fire. shutdown() is still the primary flush path.
        exportIntervalMillis: 1000,
      }),
    ],
  });
  api.metrics.setGlobalMeterProvider(meterProvider);

  // --- Logs
  const loggerProvider = new logsSDK.LoggerProvider({
    processors: [
      new logsSDK.BatchLogRecordProcessor(new logExp.OTLPLogExporter()),
    ],
  });
  api.logs.setGlobalLoggerProvider(loggerProvider);

  let shuttingDown = false;
  async function shutdown() {
    if (shuttingDown) return;
    shuttingDown = true;
    await Promise.allSettled([
      tracerProvider.shutdown(),
      meterProvider.shutdown(),
      loggerProvider.shutdown(),
    ]);
  }
  process.on('beforeExit', () => {
    shutdown();
  });

  return {
    tracer: api.trace.getTracer('defrost.example'),
    meter: api.metrics.getMeter('defrost.example'),
    logger: api.logs.getLogger('defrost.example'),
    shutdown,
  };
}

module.exports = process.env.OTEL_EXPORTER_OTLP_ENDPOINT
  ? realExports()
  : noopExports();
