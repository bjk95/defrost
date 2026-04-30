import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import {
  FailureScreen,
  MetricsEmpty,
  RunsEmpty,
  TestsEmpty,
  failureKindFromMessage,
} from "./EmptyStates";

describe("failureKindFromMessage", () => {
  it("detects auth-required from common git error phrases", () => {
    expect(failureKindFromMessage("fatal: Authentication failed")).toBe(
      "auth-required",
    );
    expect(failureKindFromMessage("could not read Username for ...")).toBe(
      "auth-required",
    );
    expect(failureKindFromMessage("remote: Repository not found.")).toBe(
      "auth-required",
    );
  });

  it("detects empty-repo when the data branch is missing", () => {
    expect(
      failureKindFromMessage(
        "warning: remote branch '_defrost' not found in upstream origin",
      ),
    ).toBe("empty-repo");
  });

  it("falls back to clone-failed", () => {
    expect(
      failureKindFromMessage("fatal: unable to access ...: timed out"),
    ).toBe("clone-failed");
    expect(failureKindFromMessage("anything else")).toBe("clone-failed");
  });
});

describe("FailureScreen", () => {
  it("renders clone-failed copy + raw stderr", () => {
    render(<FailureScreen kind="clone-failed" stderr="boom" />);
    expect(screen.getByText("Couldn't clone _defrost branch")).toBeTruthy();
    expect(screen.getByText("Boot failed")).toBeTruthy();
    expect(screen.getByText("boom")).toBeTruthy();
  });

  it("shows the GITHUB_TOKEN snippet on auth-required", () => {
    render(<FailureScreen kind="auth-required" />);
    expect(screen.getByText("Git couldn't authenticate to origin")).toBeTruthy();
    expect(screen.getAllByText(/GITHUB_TOKEN/).length).toBeGreaterThan(0);
  });

  it("uses the connected accent for empty-repo", () => {
    render(<FailureScreen kind="empty-repo" />);
    expect(screen.getByText("No history recorded yet")).toBeTruthy();
    expect(screen.getByText("First run")).toBeTruthy();
    expect(screen.getByText("Show quickstart")).toBeTruthy();
  });
});

describe("in-app empty states", () => {
  it("TestsEmpty leads with the defrost exec quickstart", () => {
    render(<TestsEmpty />);
    expect(screen.getByText("No tests recorded yet")).toBeTruthy();
    expect(screen.getByText("defrost exec go test ./...")).toBeTruthy();
  });

  it("RunsEmpty explains the timeline metaphor", () => {
    render(<RunsEmpty />);
    expect(screen.getByText("No runs to show")).toBeTruthy();
    expect(screen.getByText(/oldest/)).toBeTruthy();
    expect(screen.getByText(/now/)).toBeTruthy();
  });

  it("MetricsEmpty shows both python + go snippets", () => {
    render(<MetricsEmpty />);
    expect(screen.getByText("No metrics emitted yet")).toBeTruthy();
    expect(screen.getByText("python")).toBeTruthy();
    expect(screen.getByText("go")).toBeTruthy();
  });
});
