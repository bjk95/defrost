import { describe, it, expect } from "vitest";
import { renderWithProviders } from "@/test-utils";
import { RunCell } from "./RunCell";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useSearchParams } from "react-router-dom";

function SearchParamsProbe() {
  const [params] = useSearchParams();
  return <div data-testid="search-params">{params.toString()}</div>;
}

describe("RunCell", () => {
  it("colors by status", () => {
    renderWithProviders(<RunCell testId="t1" runId="r1" status="fail" />);
    const cell = screen.getByTestId("run-cell-t1-r1");
    expect(cell.className).toContain("bg-red-500");
  });

  it("uses neutral when no status (missing run)", () => {
    renderWithProviders(<RunCell testId="t1" runId="r1" status={null} />);
    const cell = screen.getByTestId("run-cell-t1-r1");
    expect(cell.className).toContain("bg-neutral-100");
  });

  it("click updates ?run=&test=", async () => {
    const user = userEvent.setup();
    renderWithProviders(
      <>
        <RunCell testId="tid-A" runId="run-2" status="pass" />
        <SearchParamsProbe />
      </>,
      { router: { initialEntries: ["/"] } }
    );
    await user.click(screen.getByTestId("run-cell-tid-A-run-2"));
    const probe = await screen.findByTestId("search-params");
    expect(probe.textContent).toContain("test=tid-A");
    expect(probe.textContent).toContain("run=run-2");
  });
});
