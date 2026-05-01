import { test, it } from "vitest";

test.skip("skipped via test.skip", () => {});

test.todo("todo placeholder");

it.skip("skipped via it.skip", () => {});
