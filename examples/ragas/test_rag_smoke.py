"""Hermetic smoke test for the defrost RAGAS plugin.

Bypasses ``ragas.evaluate`` (which would require an LLM API key) and writes
the same JSON shape ``result.to_pandas().to_json(orient='records')`` would
produce. Exercises the records-orient JSON contract the Go-side plugin
parses, without taking RAGAS as a Python dependency for CI.

When run under defrost the plugin emits one gauge metric per (row × scorer)
and the wrapper persists the run as usual. When run as plain pytest the
``DEFROST_RAGAS_OUT`` env var is absent and the dump is a no-op.
"""

import json
import os


def test_writes_canned_ragas_payload():
    rows = [
        {
            "user_input": "What is the capital of France?",
            "response": "Paris.",
            "retrieved_contexts": ["France's capital is Paris."],
            "reference": "Paris",
            "faithfulness": 0.92,
            "answer_relevancy": 0.88,
        },
        {
            "user_input": "Who wrote Hamlet?",
            "response": "Shakespeare.",
            "retrieved_contexts": ["William Shakespeare wrote Hamlet."],
            "reference": "William Shakespeare",
            "faithfulness": 0.31,
        },
    ]
    if path := os.environ.get("DEFROST_RAGAS_OUT"):
        with open(path, "w", encoding="utf-8") as f:
            json.dump(rows, f)
    # No ragas.evaluate result to assert on; the test passes by completing
    # without exception. The metric values appear in the defrost run
    # regardless of pytest's pass/fail outcome — that's the point of having
    # the pytest runner own pass/fail and the plugin own metrics.
    assert True
