test.skip("skipped via test.skip", () => {
  expect(true).toBe(false);
});

test.todo("todo placeholder");

xit("skipped via xit", () => {
  expect(true).toBe(false);
});
