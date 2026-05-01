import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderWithProviders } from "@/test-utils";
import { TestDetailPage } from "./TestDetailPage";
import * as api from "@/api";

vi.mock("@/api");

describe("TestDetailPage", () => {
  beforeEach(() => {
    vi.mocked(api.getTests).mockResolvedValue({ runs: [], tests: [] });
  });

  it("renders without throwing when test id is not found", () => {
    const { container } = renderWithProviders(<TestDetailPage />, {
      router: { initialEntries: ["/test?id=pkg.TestA"] },
    });
    expect(container).toBeTruthy();
  });

  it("renders something for missing test", () => {
    const { container } = renderWithProviders(<TestDetailPage />, {
      router: { initialEntries: ["/test?id=nonexistent"] },
    });
    expect(container.firstChild).toBeTruthy();
  });
});
