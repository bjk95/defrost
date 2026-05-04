# Concepts

Background reading. These pages explain how defrost works and why it's
built the way it is — read these before wiring defrost into a real
project so the trade-offs make sense.

- **[Git as the database](./git-as-database.md)** — why defrost stores
  history in commits instead of a service, and the trade-offs.
- **[The `_defrost` branch](./defrost-branch.md)** — lifecycle, what
  gets committed, and how to inspect it by hand.
- **[OpenTelemetry as the ingestion API](./otel-as-ingestion.md)** — why
  defrost speaks OTel rather than shipping a client library.
- **[Suppression model](./suppression.md)** — how suppressed tests
  differ from skipped ones, and why the distinction matters.
