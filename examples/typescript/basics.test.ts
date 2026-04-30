test("adds correctly", () => {
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
