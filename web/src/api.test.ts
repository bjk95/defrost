import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { getTests, getTestRun } from "./api";

describe("api.getTests", () => {
  const originalFetch = global.fetch;
  afterEach(() => { global.fetch = originalFetch; });

  it("parses GridResponse from /api/tests", async () => {
    global.fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ runs: [], tests: [] }), { status: 200 })
    );
    const r = await getTests();
    expect(r).toEqual({ runs: [], tests: [] });
    expect((global.fetch as any).mock.calls[0][0]).toBe("/api/tests");
  });

  it("throws on non-2xx", async () => {
    global.fetch = vi.fn().mockResolvedValue(new Response("nope", { status: 500 }));
    await expect(getTests()).rejects.toThrow();
  });
});

describe("api.getTestRun", () => {
  beforeEach(() => {
    global.fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ test: {}, run: {} }), { status: 200 })
    );
  });
  it("hits the right URL", async () => {
    await getTestRun("tid-A", "run-1");
    expect((global.fetch as any).mock.calls[0][0]).toBe("/api/test/tid-A/run/run-1");
  });
  it("URL-encodes its arguments", async () => {
    await getTestRun("tid/with slash", "run/1");
    expect((global.fetch as any).mock.calls[0][0]).toBe("/api/test/tid%2Fwith%20slash/run/run%2F1");
  });
});
