import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Mock posthog-js before importing the module under test so the module's
// top-level `import posthog from "posthog-js"` resolves to the mock.
vi.mock("posthog-js", () => {
  const mock = {
    init: vi.fn(),
    register: vi.fn(),
    reset: vi.fn(),
    identify: vi.fn(),
    capture: vi.fn(),
  };
  return { default: mock };
});

// Re-import per test so module-level `initialized` / cached super-props
// don't leak between cases.
async function loadModule() {
  vi.resetModules();
  const analytics = await import("./index");
  const posthog = (await import("posthog-js")).default as unknown as {
    init: ReturnType<typeof vi.fn>;
    register: ReturnType<typeof vi.fn>;
    reset: ReturnType<typeof vi.fn>;
  };
  posthog.init.mockClear();
  posthog.register.mockClear();
  posthog.reset.mockClear();
  return { analytics, posthog };
}

beforeEach(() => {
  vi.stubGlobal("window", {});
  vi.stubGlobal("navigator", { userAgent: "Mozilla/5.0" });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("initAnalytics super-properties", () => {
  it("registers client_type and app_version after posthog.init", async () => {
    const { analytics, posthog } = await loadModule();
    analytics.initAnalytics({ key: "k", host: "", appVersion: "1.2.3" });
    expect(posthog.register).toHaveBeenCalledWith({
      client_type: "web",
      app_version: "1.2.3",
    });
  });

  it("omits app_version when not provided", async () => {
    const { analytics, posthog } = await loadModule();
    analytics.initAnalytics({ key: "k", host: "" });
    expect(posthog.register).toHaveBeenCalledWith({ client_type: "web" });
  });

  it("detects desktop when window.electron is present", async () => {
    vi.stubGlobal("window", { electron: {} });
    const { analytics, posthog } = await loadModule();
    analytics.initAnalytics({ key: "k", host: "" });
    expect(posthog.register).toHaveBeenCalledWith({ client_type: "desktop" });
  });
});

describe("resetAnalytics", () => {
  it("re-registers super-properties after reset so subsequent events keep client_type", async () => {
    const { analytics, posthog } = await loadModule();
    analytics.initAnalytics({ key: "k", host: "", appVersion: "1.2.3" });
    posthog.register.mockClear();

    analytics.resetAnalytics();

    // reset() wipes persisted super-props; we re-register the cached set so
    // the next session's events keep client_type + app_version.
    expect(posthog.reset).toHaveBeenCalledTimes(1);
    expect(posthog.register).toHaveBeenCalledWith({
      client_type: "web",
      app_version: "1.2.3",
    });
  });

  it("is a no-op when analytics was never initialized", async () => {
    const { analytics, posthog } = await loadModule();
    analytics.resetAnalytics();
    expect(posthog.reset).not.toHaveBeenCalled();
    expect(posthog.register).not.toHaveBeenCalled();
  });
});
