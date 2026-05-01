import { describe, it, expect } from "vitest";
import { queryClient } from "./queryClient";

describe("queryClient defaults", () => {
  it("staleTime is 60s", () => {
    const opts = queryClient.getDefaultOptions();
    expect(opts.queries?.staleTime).toBe(60_000);
  });
  it("gcTime is 5m", () => {
    const opts = queryClient.getDefaultOptions();
    expect(opts.queries?.gcTime).toBe(5 * 60_000);
  });
  it("retry is 1", () => {
    const opts = queryClient.getDefaultOptions();
    expect(opts.queries?.retry).toBe(1);
  });
  it("refetchOnWindowFocus disabled", () => {
    const opts = queryClient.getDefaultOptions();
    expect(opts.queries?.refetchOnWindowFocus).toBe(false);
  });
});
