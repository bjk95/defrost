// eslint-disable-next-line @typescript-eslint/no-var-requires
const otel = require("../_otel-setup");

test("adds correctly", () => {
  // When run under `defrost exec`, emit one OTel span, one metric
  // data point, and one log record so CI's readback assertion can
  // verify all three signals reach origin/_defrost. Standalone
  // `npm test` short-circuits the otel module to a no-op.
  if (otel.tracer) {
    const span = otel.tracer.startSpan("defrost.example.jest-ts.add");
    otel.meter
      .createCounter("example.test.invocations")
      .add(1, { language: "typescript", runner: "jest" });
    otel.logger.emit({
      severityText: "INFO",
      body: "jest-ts example: emitted via OTel SDK",
    });
    span.end();
  }
  const result: number = 1 + 1;
  expect(result).toBe(2);
});

test("intentional failure", () => {
  const result: number = 1 + 1;
  expect(result).toBe(3);
});

test("async pass", async () => {
  await new Promise<void>((r) => setTimeout(r, 1));
  expect(true).toBe(true);
});

test("async fail", async () => {
  await new Promise<void>((r) => setTimeout(r, 1));
  throw new Error("intentional async failure");
});
