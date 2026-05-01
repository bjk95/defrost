import { describe, test, expect } from "vitest";

describe("math", () => {
  describe("addition", () => {
    test("adds correctly", () => {
      expect(1 + 1).toBe(2);
    });
    test("intentional fail", () => {
      expect(2 + 2).toBe(5);
    });
  });
});
