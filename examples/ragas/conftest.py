"""Session-scoped collector for RAGAS evaluation results.

Drop this file (or merge its contents) into your repo's conftest.py to make
``ragas.evaluate`` results visible to defrost. Each test that runs
``ragas.evaluate(...)`` extends ``ragas_rows`` with the dataset's rows;
``pytest_sessionfinish`` writes the aggregated array to
``$DEFROST_RAGAS_OUT`` once per session.

The hook is a no-op when the env var is unset, so the same conftest is
safe to keep checked in: ``pytest`` runs identically with or without
``defrost exec``.
"""

import json
import os

import pytest

_rows: list[dict] = []


@pytest.fixture
def ragas_rows() -> list[dict]:
    """Append result rows here from inside tests.

    Usage::

        def test_rag(ragas_rows):
            result = evaluate(dataset, metrics=[faithfulness])
            ragas_rows.extend(result.to_pandas().to_dict(orient="records"))
            assert result["faithfulness"] > 0.7
    """
    return _rows


def pytest_sessionfinish(session, exitstatus):
    path = os.environ.get("DEFROST_RAGAS_OUT")
    if not path or not _rows:
        return
    with open(path, "w", encoding="utf-8") as f:
        json.dump(_rows, f)
