import logging
import os

import pytest


@pytest.fixture
def good_fixture():
    return 42


@pytest.fixture
def broken_fixture():
    raise ValueError("intentional setup failure")


# OTel log emission. Wires Python's stdlib logging to an OTLP/HTTP log
# exporter when defrost has set OTEL_EXPORTER_OTLP_ENDPOINT in the
# environment. Standalone `pytest examples/python/` keeps working
# unchanged — the OTel setup short-circuits when that env var is
# absent (and silently no-ops if the opentelemetry-* packages
# themselves aren't installed).
#
# Set up in pytest_sessionstart, NOT pytest_configure: pytest's own
# logging plugin attaches handlers in pytest_configure and ours can
# get shadowed if added too early. sessionstart runs after every
# plugin's configure hook so the log handler we add here actually
# sticks.
_otel_provider = None


def pytest_sessionstart(session):
    global _otel_provider
    if _otel_provider is not None:
        return
    if not os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT"):
        return
    try:
        from opentelemetry import _logs
        from opentelemetry.exporter.otlp.proto.http._log_exporter import (
            OTLPLogExporter,
        )
        from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
        from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
    except ImportError:
        # opentelemetry-* not installed in the current environment;
        # skip silently. The OTel-logs integration test in CI installs
        # the deps explicitly.
        return

    _otel_provider = LoggerProvider()
    _otel_provider.add_log_record_processor(
        BatchLogRecordProcessor(OTLPLogExporter())
    )
    _logs.set_logger_provider(_otel_provider)

    handler = LoggingHandler(level=logging.DEBUG, logger_provider=_otel_provider)
    handler.setLevel(logging.DEBUG)
    root = logging.getLogger()
    root.addHandler(handler)
    if root.level == logging.NOTSET or root.level > logging.DEBUG:
        root.setLevel(logging.DEBUG)


def pytest_sessionfinish(session, exitstatus):
    # BatchLogRecordProcessor's default flush interval is 5s; defrost
    # only waits 2s after the child exits for stragglers. Force a
    # synchronous flush before pytest exits so the last batch isn't
    # lost.
    if _otel_provider is not None:
        _otel_provider.shutdown()
