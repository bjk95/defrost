import "@testing-library/jest-dom/vitest";

// jsdom doesn't implement ResizeObserver; supply a no-op stub so charts
// that observe their container can render in unit tests.
if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}
