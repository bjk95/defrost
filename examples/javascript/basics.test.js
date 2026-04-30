test("adds correctly", () => {
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
