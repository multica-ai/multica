import "@testing-library/jest-dom/vitest";
import "@multica/vitest-setup/web-storage";

// Everything below patches gaps in jsdom. Pure-logic suites opt out of jsdom
// with `// @vitest-environment node` and share this file, so there is no DOM to
// patch there — bail out rather than guard each stub.
if (typeof window !== "undefined") {
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
}
