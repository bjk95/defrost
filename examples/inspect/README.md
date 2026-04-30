# Inspect AI dogfood

Defrost dogfood for the Inspect AI adapter (Role A runner).

## Setup

```sh
pip install inspect-ai
```

## Run

```sh
defrost exec inspect eval examples/inspect/task.py
```

## What it tests

Two samples scored with Inspect's built-in `match()` scorer:

| Sample | Mock answer | Target | Outcome |
|---|---|---|---|
| 1 | `Paris` | `Paris` | pass |
| 2 | `London` | `Berlin` | fail (intentional, suppressed in CI) |

A `mock_solver` returns each sample's `metadata.mock_answer`, so the run
is fully hermetic — no LLM API calls.
