import { test, expect } from "vitest";
import otel from "../_otel-setup.js";

test("adds correctly", () => {
  // When run under `defrost exec`, emit one OTel span, one metric
  // data point, and one log record so CI's readback assertion can
  // verify all three signals reach origin/_defrost. Standalone
  // `npm test` short-circuits the otel module to a no-op.
  if (otel.tracer) {
    const span = otel.tracer.startSpan("defrost.example.vitest.add");
    otel.meter
      .createCounter("example.test.invocations")
      .add(1, { language: "javascript", runner: "vitest" });
    otel.logger.emit({
      severityText: "INFO",
      body: "vitest example: emitted via OTel SDK",
    });
    span.end();
  }
  expect(1 + 1).toBe(2);
});

test("intentional failure", () => {
  expect(1 + 1).toBe(3);
});

test("async pass", async () => {
  await new Promise((r) => setTimeout(r, 1));
  expect(true).toBe(true);
});

test("async fail", async () => {
  await new Promise((r) => setTimeout(r, 1));
  throw new Error("intentional async failure");
});
