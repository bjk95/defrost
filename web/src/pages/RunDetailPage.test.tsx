import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderWithProviders } from "@/test-utils";
import { RunDetailPage } from "./RunDetailPage";
import * as api from "@/api";

vi.mock("@/api");

describe("RunDetailPage", () => {
  beforeEach(() => {
    vi.mocked(api.getTests).mockResolvedValue({ runs: [], tests: [] });
  });

  it("renders without throwing when run id is not found", () => {
    const { container } = renderWithProviders(<RunDetailPage />, {
      router: { initialEntries: ["/run?id=r1"] },
    });
    expect(container).toBeTruthy();
  });

  it("renders something for missing run", () => {
    const { container } = renderWithProviders(<RunDetailPage />, {
      router: { initialEntries: ["/run?id=nonexistent"] },
    });
    expect(container.firstChild).toBeTruthy();
  });
});
