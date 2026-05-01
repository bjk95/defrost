import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { Icon, Logo } from "./Icons";

describe("Icon", () => {
  it("exports an object with icon components", () => {
    expect(typeof Icon).toBe("object");
    expect(Object.keys(Icon).length).toBeGreaterThan(0);
  });

  it("each icon renders without throwing", () => {
    for (const [name, Component] of Object.entries(Icon)) {
      const { container } = render(<Component key={name} />);
      expect(container).toBeTruthy();
    }
  });
});

describe("Logo", () => {
  it("renders without throwing", () => {
    const { container } = render(<Logo />);
    expect(container).toBeTruthy();
  });

  it("renders with a custom size", () => {
    const { container } = render(<Logo size={32} />);
    expect(container).toBeTruthy();
  });
});
