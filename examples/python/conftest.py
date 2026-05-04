import logging
import os

import pytest


@pytest.fixture
def good_fixture():
    return 42


@pytest.fixture
def broken_fixture():
    raise ValueError("intentional setup failure")


# OTel SDK emission. Wires custom traces, metrics, and logs to the
# OTLP/HTTP endpoint that `defrost exec` sets via
# OTEL_EXPORTER_OTLP_ENDPOINT. Standalone `pytest examples/python/`
# keeps working unchanged — the OTel setup short-circuits when that
# env var is absent (and silently no-ops if the opentelemetry-*
# packages themselves aren't installed).
#
# Three signals × one provider each. Each provider's `shutdown()`
# forces a synchronous flush before the child exits — defrost only
# waits 2s after the child for stragglers, and the default batch
# intervals are longer than that.
#
# Setup is in pytest_sessionstart, NOT pytest_configure: pytest's own
# logging plugin attaches handlers in pytest_configure and ours can
# get shadowed if added too early. sessionstart runs after every
# plugin's configure hook so the log handler we add here actually
# sticks.
_tracer_provider = None
_meter_provider = None
_logger_provider = None


def pytest_sessionstart(session):
    global _tracer_provider, _meter_provider, _logger_provider
    if _tracer_provider is not None:
        return
    if not os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT"):
        return
    try:
        from opentelemetry import _logs, metrics, trace
        from opentelemetry.exporter.otlp.proto.http._log_exporter import (
            OTLPLogExporter,
        )
        from opentelemetry.exporter.otlp.proto.http.metric_exporter import (
            OTLPMetricExporter,
        )
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
            OTLPSpanExporter,
        )
        from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
        from opentelemetry.sdk._logs.export import SimpleLogRecordProcessor
        from opentelemetry.sdk.metrics import MeterProvider
        from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
        from opentelemetry.sdk.trace import TracerProvider
        from opentelemetry.sdk.trace.export import SimpleSpanProcessor
    except ImportError:
        # opentelemetry-* not installed in the current environment;
        # skip silently. The CI integration job installs the deps
        # explicitly (see .github/workflows/integration.yml).
        return

    # Use Simple processors (one export per record) instead of Batch
    # variants. Defrost only waits 2s after the child for stragglers,
    # and Batch's default flush interval is 5s — even with explicit
    # shutdown() the network export can race the parent's drain
    # window. Simple is synchronous per record, so by the time the
    # test method returns the record is already in defrost's
    # receiver. Cost (O(N) HTTP calls) is fine for example workloads.

    # --- Traces
    _tracer_provider = TracerProvider()
    _tracer_provider.add_span_processor(SimpleSpanProcessor(OTLPSpanExporter()))
    trace.set_tracer_provider(_tracer_provider)

    # --- Metrics
    # PeriodicExportingMetricReader has a default 60s interval; force
    # a tight one so the first export fires inside defrost's drain
    # window even if shutdown() doesn't (it does, but belt + braces).
    _meter_provider = MeterProvider(
        metric_readers=[
            PeriodicExportingMetricReader(
                OTLPMetricExporter(), export_interval_millis=1000
            )
        ],
    )
    metrics.set_meter_provider(_meter_provider)

    # --- Logs (stdlib bridge)
    _logger_provider = LoggerProvider()
    _logger_provider.add_log_record_processor(
        SimpleLogRecordProcessor(OTLPLogExporter())
    )
    _logs.set_logger_provider(_logger_provider)
    handler = LoggingHandler(level=logging.DEBUG, logger_provider=_logger_provider)
    handler.setLevel(logging.DEBUG)
    root = logging.getLogger()
    root.addHandler(handler)
    if root.level == logging.NOTSET or root.level > logging.DEBUG:
        root.setLevel(logging.DEBUG)


def pytest_sessionfinish(session, exitstatus):
    # Force synchronous flushes before pytest exits. Simple processors
    # (used above) are already synchronous per record, but the metric
    # reader's last interval may not have fired yet — shutdown()
    # forces a final read+export.
    for p in (_tracer_provider, _meter_provider, _logger_provider):
        if p is not None:
            try:
                p.shutdown()
            except Exception:
                # Don't let SDK teardown errors mask a real test failure.
                pass


# Emit one custom span, one custom metric data point, and one log
# record per test session. Lives in a fixture (not at module load
# time) so it runs after pytest_sessionstart has installed the SDKs.
@pytest.fixture(autouse=True, scope="session")
def _defrost_otel_emit(request):
    if _tracer_provider is None:
        yield
        return
    from opentelemetry import metrics, trace

    tracer = trace.get_tracer("defrost.example.pytest")
    meter = metrics.get_meter("defrost.example.pytest")
    counter = meter.create_counter("example.test_session.runs")

    with tracer.start_as_current_span("defrost.example.pytest.session"):
        counter.add(1, {"language": "python", "runner": "pytest"})
        logging.getLogger("defrost.example.pytest").info(
            "session start: emitted via OTel SDK"
        )
        yield
