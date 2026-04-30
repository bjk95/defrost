describe("math", () => {
  describe("addition", () => {
    test("adds correctly", () => {
      expect(1 + 1).toBe(2);
    });

    test("intentional failure", () => {
      expect(1 + 1).toBe(3);
    });
  });
});
