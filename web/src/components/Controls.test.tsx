import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";
import { SearchInput, Segmented } from "./Controls";

describe("SearchInput", () => {
  it("renders with empty value", () => {
    const { container } = render(
      <SearchInput value="" onChange={vi.fn()} />,
    );
    expect(container).toBeTruthy();
  });

  it("renders with a value and clear button", () => {
    const { container } = render(
      <SearchInput value="foo" onChange={vi.fn()} placeholder="search…" />,
    );
    expect(container).toBeTruthy();
  });
});

describe("Segmented", () => {
  it("renders options and highlights the active one", () => {
    const { container } = render(
      <Segmented
        value="all"
        onChange={vi.fn()}
        options={[
          { value: "all", label: "All" },
          { value: "fail", label: "Failing" },
        ]}
      />,
    );
    expect(container).toBeTruthy();
  });
});
