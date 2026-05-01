import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { StatusPill, RunCell, CountsBar, MetaPill, Avatar, SectionLabel, Card } from "./Primitives";

describe("StatusPill", () => {
  it("renders pass status", () => {
    const { container } = render(<StatusPill status="pass" />);
    expect(container).toBeTruthy();
  });

  it("renders fail status", () => {
    const { container } = render(<StatusPill status="fail" />);
    expect(container).toBeTruthy();
  });

  it("renders unknown status without throwing", () => {
    const { container } = render(<StatusPill status="unknown" />);
    expect(container).toBeTruthy();
  });
});

describe("RunCell", () => {
  it("renders with no props", () => {
    const { container } = render(<RunCell />);
    expect(container).toBeTruthy();
  });

  it("renders with a pass status", () => {
    const { container } = render(<RunCell status="pass" />);
    expect(container).toBeTruthy();
  });
});

describe("CountsBar", () => {
  it("renders with zero counts", () => {
    const { container } = render(<CountsBar counts={{ pass: 0, fail: 0, skip: 0 }} />);
    expect(container).toBeTruthy();
  });

  it("renders with mixed counts", () => {
    const { container } = render(<CountsBar counts={{ pass: 5, fail: 2, skip: 1 }} />);
    expect(container).toBeTruthy();
  });
});

describe("MetaPill", () => {
  it("renders with just a value", () => {
    const { container } = render(<MetaPill value="hello" />);
    expect(container).toBeTruthy();
  });

  it("renders with label and value", () => {
    const { container } = render(<MetaPill label="branch" value="main" />);
    expect(container).toBeTruthy();
  });
});

describe("Avatar", () => {
  it("renders with no name", () => {
    const { container } = render(<Avatar />);
    expect(container).toBeTruthy();
  });

  it("renders with a name", () => {
    const { container } = render(<Avatar name="Alice" />);
    expect(container).toBeTruthy();
  });
});

describe("SectionLabel", () => {
  it("renders children", () => {
    const { container } = render(<SectionLabel>My Section</SectionLabel>);
    expect(container).toBeTruthy();
  });
});

describe("Card", () => {
  it("renders children", () => {
    const { container } = render(<Card>content</Card>);
    expect(container).toBeTruthy();
  });
});
