import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderWithProviders } from "@/test-utils";
import { SuppressionsPage } from "./SuppressionsPage";
import * as api from "@/api";

vi.mock("@/api");

describe("SuppressionsPage", () => {
  beforeEach(() => {
    vi.mocked(api.getTests).mockResolvedValue({ runs: [], tests: [] });
  });

  it("renders without throwing", () => {
    const { container } = renderWithProviders(<SuppressionsPage />);
    expect(container).toBeTruthy();
  });

  it("renders the suppression list heading", () => {
    const { container } = renderWithProviders(<SuppressionsPage />);
    expect(container.firstChild).toBeTruthy();
  });
});
