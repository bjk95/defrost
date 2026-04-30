"""defrost helper for RAGAS evaluations.

Call ``write_results(result)`` immediately after ``ragas.evaluate(...)``::

    from ragas import evaluate
    from ragas.metrics import faithfulness, answer_relevancy
    import defrost_ragas

    def test_rag_scores():
        result = evaluate(dataset, metrics=[faithfulness, answer_relevancy])
        defrost_ragas.write_results(result)
        assert result["faithfulness"] > 0.7

When the test runs under defrost, the helper serialises the EvaluationResult
to ``$DEFROST_RAGAS_OUT`` (a defrost-controlled tempfile). When defrost is not
active the env var is absent and the call is a no-op, so the same test file
runs cleanly inside and outside the wrapper.
"""

from __future__ import annotations

import json
import math
import os
from typing import Any, Iterable, Mapping


# Columns the dataset carries that aren't metric scores. RAGAS appends each
# scored metric as a new column to the same DataFrame, so we identify metrics
# by exclusion rather than by enumeration of known scorer names. New metrics
# the user adds will work without code changes here.
_NON_METRIC_COLUMNS = frozenset(
    {
        "question",
        "answer",
        "contexts",
        "ground_truth",
        "ground_truths",
        "reference",
        "reference_contexts",
        "retrieved_contexts",
        "user_input",
        "response",
    }
)


def write_results(result: Any) -> None:
    """Serialise a ragas.EvaluationResult to ``$DEFROST_RAGAS_OUT``.

    No-op when ``DEFROST_RAGAS_OUT`` is unset (i.e. defrost is not wrapping
    the test run) so user tests stay portable.
    """
    out_path = os.environ.get("DEFROST_RAGAS_OUT")
    if not out_path:
        return

    df = result.to_pandas()
    metric_cols = [c for c in df.columns if c not in _NON_METRIC_COLUMNS]

    rows = []
    for i, row in df.iterrows():
        scores = {}
        for col in metric_cols:
            value = row[col]
            try:
                f = float(value)
            except (TypeError, ValueError):
                continue
            # NaN means RAGAS couldn't score this row (e.g. empty context).
            # Drop the entry rather than emit a non-JSON sentinel — the Go
            # parser only sees clean (row × scorer) pairs.
            if math.isnan(f):
                continue
            scores[col] = f
        entry: dict[str, Any] = {"row_index": int(i), "scores": scores}
        if "question" in df.columns:
            entry["question"] = str(row["question"])
        rows.append(entry)

    _write_raw({"ragas_version": _ragas_version(), "rows": rows})


def _write_raw(payload: Mapping[str, Any]) -> None:
    """Write a pre-built payload dict to ``$DEFROST_RAGAS_OUT``.

    Exposed so hermetic dogfood tests can bypass ``ragas.evaluate`` and
    write canned scores directly. Not part of the public user API.
    """
    out_path = os.environ.get("DEFROST_RAGAS_OUT")
    if not out_path:
        return
    with open(out_path, "w", encoding="utf-8") as f:
        json.dump(payload, f)


def _ragas_version() -> str:
    try:
        import ragas  # type: ignore[import-not-found]

        return getattr(ragas, "__version__", "unknown")
    except ImportError:
        return "unknown"


__all__: Iterable[str] = ("write_results",)
