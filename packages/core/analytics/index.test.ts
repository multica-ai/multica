import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Mock @amplitude/analytics-browser before importing the module under test.
vi.mock("@amplitude/analytics-browser", () => {
  const Identify = vi.fn().mockImplementation(() => ({
    set: vi.fn(),
    setOnce: vi.fn(),
  }));
  const mock = {
    init: vi.fn(),
    track: vi.fn(),
    identify: vi.fn(),
    setUserId: vi.fn(),
    reset: vi.fn(),
    Identify,
    Types: {},
  };
  return mock;
});

// Re-import per test so module-level `initialized` / cached state
// don't leak between cases.
async function loadModule() {
  vi.resetModules();
  const analytics = await import("./index");
  const amp = (await import("@amplitude/analytics-browser")) as unknown as {
    init: ReturnType<typeof vi.fn>;
    track: ReturnType<typeof vi.fn>;
    identify: ReturnType<typeof vi.fn>;
    setUserId: ReturnType<typeof vi.fn>;
    reset: ReturnType<typeof vi.fn>;
  };
  amp.init.mockClear();
  amp.track.mockClear();
  amp.identify.mockClear();
  amp.setUserId.mockClear();
  amp.reset.mockClear();
  return { analytics, amp };
}

beforeEach(() => {
  vi.stubGlobal("window", {});
  vi.stubGlobal("navigator", { userAgent: "Mozilla/5.0" });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("initAnalytics", () => {
  it("calls amplitude.init with the API key", async () => {
    const { analytics, amp } = await loadModule();
    analytics.initAnalytics({ key: "amp_test", host: "", appVersion: "1.2.3" });
    expect(amp.init).toHaveBeenCalledWith("amp_test", {
      autocapture: false,
      defaultTracking: false,
      appVersion: "1.2.3",
    });
  });

  it("omits appVersion when not provided", async () => {
    const { analytics, amp } = await loadModule();
    analytics.initAnalytics({ key: "amp_test", host: "" });
    expect(amp.init).toHaveBeenCalledWith("amp_test", {
      autocapture: false,
      defaultTracking: false,
      appVersion: undefined,
    });
  });

  it("returns false when no key is provided", async () => {
    const { analytics } = await loadModule();
    expect(analytics.initAnalytics({ key: "", host: "" })).toBe(false);
  });
});

describe("detectClientType", () => {
  it("detects desktop when window.electron is present", async () => {
    vi.stubGlobal("window", { electron: {} });
    const { analytics } = await loadModule();
    expect(analytics.detectClientType()).toBe("desktop");
  });

  it("detects web by default", async () => {
    const { analytics } = await loadModule();
    expect(analytics.detectClientType()).toBe("web");
  });
});

describe("resetAnalytics", () => {
  it("calls amplitude.reset() when initialized", async () => {
    const { analytics, amp } = await loadModule();
    analytics.initAnalytics({ key: "k", host: "" });
    analytics.resetAnalytics();
    expect(amp.reset).toHaveBeenCalledTimes(1);
  });

  it("is a no-op when analytics was never initialized", async () => {
    const { analytics, amp } = await loadModule();
    analytics.resetAnalytics();
    expect(amp.reset).not.toHaveBeenCalled();
  });
});
