test.each([
  [1, 1, 2],
  [2, 2, 4],
  [3, 3, 7], // intentional failure
])("adds %i + %i = %i", (a, b, expected) => {
  expect(a + b).toBe(expected);
});
