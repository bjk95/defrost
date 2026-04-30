"""Hermetic smoke test for the defrost RAGAS plugin.

Bypasses ``ragas.evaluate`` (which would require an LLM API key) and writes
canned scores directly via ``defrost_ragas._write_raw``. Exercises the same
JSON contract the production helper produces, so the Go-side plugin sees a
realistic payload.

When run under defrost, the plugin emits one gauge metric per (row × scorer)
and the wrapper persists the run as usual. When run as plain pytest the
``DEFROST_RAGAS_OUT`` env var is absent and the writes are no-ops.

The whole module writes one payload covering both rows because the helper
overwrites ``$DEFROST_RAGAS_OUT`` on each call (spec §11.5). Splitting
across multiple tests would silently lose all but the last row's metrics.
"""

import defrost_ragas


def test_writes_canned_ragas_payload():
    defrost_ragas._write_raw(
        {
            "ragas_version": "test",
            "rows": [
                {
                    "row_index": 0,
                    "question": "What is the capital of France?",
                    "scores": {"faithfulness": 0.92, "answer_relevancy": 0.88},
                },
                {
                    "row_index": 1,
                    "question": "Who wrote Hamlet?",
                    "scores": {"faithfulness": 0.31},
                },
            ],
        }
    )
    # No ragas.evaluate result to assert on; the test passes by completing
    # without exception. The metric values appear in the defrost run
    # regardless of pytest's pass/fail outcome — that's the point of having
    # the pytest runner own pass/fail and the plugin own metrics.
    assert True
