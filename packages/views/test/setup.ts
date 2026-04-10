import "@testing-library/jest-dom/vitest";

// jsdom doesn't provide ResizeObserver; stub it so components that rely on it
// (e.g. input-otp) can render in tests.
if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
}

// jsdom doesn't implement elementFromPoint; input-otp uses it internally.
if (typeof document.elementFromPoint !== "function") {
  document.elementFromPoint = () => null;
}
