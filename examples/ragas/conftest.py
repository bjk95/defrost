"""Set-and-forget capture for RAGAS evaluation results.

Drop this file (or merge its contents) into your repo's top-level
conftest.py and never touch it again. At conftest import time, before any
test module is loaded, ``ragas.evaluate`` (and ``ragas.aevaluate`` when
present) is monkey-patched to record each result's rows into a
session-scoped accumulator. ``pytest_sessionfinish`` writes the
aggregated JSON array to ``$DEFROST_RAGAS_OUT`` once at the end of the
run.

User tests are unchanged from how they'd be written without defrost:

    from ragas import evaluate
    from ragas.metrics import faithfulness

    def test_rag():
        result = evaluate(dataset, metrics=[faithfulness])
        assert result["faithfulness"] > 0.7

No imports from defrost. No fixture parameters. No per-test glue.

The hook is a no-op when ``DEFROST_RAGAS_OUT`` is unset, so the same
conftest is safe to keep checked in: ``pytest`` runs identically with or
without the ``defrost exec`` wrapper.
"""

import json
import os

_rows: list[dict] = []
_patched = False


def _capture(result) -> None:
    """Append result rows to the session aggregator. Defensive: any failure
    here must not break the user's test, so we swallow exceptions silently —
    the worst outcome is a missing data point, not a failed run.
    """
    try:
        _rows.extend(result.to_pandas().to_dict(orient="records"))
    except Exception:
        pass


def _install_capture() -> None:
    """Monkey-patch ragas.evaluate so every call across the test suite is
    recorded. Runs once at conftest import, before pytest collects any test
    modules — so ``from ragas import evaluate`` at a test module's top
    level binds to the wrapped function rather than the original.
    """
    global _patched
    if _patched:
        return
    try:
        import ragas
    except ImportError:
        return

    if callable(getattr(ragas, "evaluate", None)):
        orig = ragas.evaluate

        def wrapped(*args, **kwargs):
            result = orig(*args, **kwargs)
            _capture(result)
            return result

        ragas.evaluate = wrapped

    if callable(getattr(ragas, "aevaluate", None)):
        orig_async = ragas.aevaluate

        async def wrapped_async(*args, **kwargs):
            result = await orig_async(*args, **kwargs)
            _capture(result)
            return result

        ragas.aevaluate = wrapped_async

    _patched = True


_install_capture()


def pytest_sessionfinish(session, exitstatus):
    path = os.environ.get("DEFROST_RAGAS_OUT")
    if not path or not _rows:
        return
    with open(path, "w", encoding="utf-8") as f:
        json.dump(_rows, f)
