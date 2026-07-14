// Hard gate (MUL-4466 §6.2): the desktop renderer MUST install the perf-recorder
// DevTools hook before react-dom is evaluated, and it must be proven by an
// automated test, not code review. main.tsx installs the hook, then dynamically
// imports ./app-bootstrap (which owns the react-dom import). Both dependencies
// are mocked so this test asserts pure ordering without loading React/Electron.
import { afterEach, describe, expect, it, vi } from "vitest";

// Per-test mocks (vi.doMock, not hoisted) so each scenario re-runs the mock
// factories after resetModules and records a fresh ordering trace.
function installMocks(order: string[]): void {
  vi.doMock("@multica/perf-recorder", () => ({
    installRecorderHook: () => {
      order.push("installRecorderHook");
    },
    createRecorder: () => {
      order.push("createRecorder");
      return { recorder: {}, panel: { destroy() {} }, destroy() {} };
    },
  }));
  vi.doMock("./app-bootstrap", () => {
    // Stands in for the real module whose first import is `react-dom/client`.
    order.push("app-bootstrap");
    return {};
  });
}

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
  vi.doUnmock("@multica/perf-recorder");
  vi.doUnmock("./app-bootstrap");
});

describe("desktop renderer two-stage bootstrap", () => {
  it("installs the recorder hook before app-bootstrap (react-dom) is imported", async () => {
    const order: string[] = [];
    vi.resetModules();
    installMocks(order);
    vi.stubEnv("VITE_PERF_RECORDER", "1");

    await import("./main");
    await vi.waitFor(() => expect(order).toContain("app-bootstrap"));

    const hookIndex = order.indexOf("installRecorderHook");
    const appIndex = order.indexOf("app-bootstrap");
    expect(hookIndex).toBeGreaterThanOrEqual(0);
    expect(hookIndex).toBeLessThan(appIndex);
  });

  it("does not load the recorder when the dev flag is off, but still boots the app", async () => {
    const order: string[] = [];
    vi.resetModules();
    installMocks(order);
    // VITE_PERF_RECORDER unset — production / opt-out path.

    await import("./main");
    await vi.waitFor(() => expect(order).toContain("app-bootstrap"));
    expect(order).not.toContain("installRecorderHook");
  });
});
