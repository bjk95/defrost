import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderWithProviders } from "@/test-utils";
import { RunsPage } from "./RunsPage";
import * as api from "@/api";

vi.mock("@/api");

describe("RunsPage", () => {
  beforeEach(() => {
    vi.mocked(api.getTests).mockResolvedValue({ runs: [], tests: [] });
  });

  it("renders without throwing", () => {
    const { container } = renderWithProviders(<RunsPage />);
    expect(container).toBeTruthy();
  });

  it("renders something for empty data", () => {
    const { container } = renderWithProviders(<RunsPage />);
    expect(container.firstChild).toBeTruthy();
  });
});
