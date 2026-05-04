import logging


def test_pass():
    print("captured stdout from test_pass")
    # Emits an OTel log record when run under `defrost exec` (the
    # conftest's pytest_configure wires up the OTLP log exporter
    # whenever the OTEL_EXPORTER_OTLP_ENDPOINT env var is set).
    # Standalone pytest treats this as a no-op stdlib log call.
    logging.getLogger(__name__).info("test_pass: emitted from a test")
    assert 1 == 1


def test_fail():
    assert 1 == 2


def test_raises():
    raise RuntimeError("intentional non-assertion failure")
