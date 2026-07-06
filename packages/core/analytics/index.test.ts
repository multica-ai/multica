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
    captureException: vi.fn(),
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
    captureException: ReturnType<typeof vi.fn>;
  };
  posthog.init.mockClear();
  posthog.register.mockClear();
  posthog.reset.mockClear();
  posthog.captureException.mockClear();
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
      environment: "dev",
      event_schema_version: 2,
      is_demo: false,
    });
  });

  it("omits app_version when not provided", async () => {
    const { analytics, posthog } = await loadModule();
    analytics.initAnalytics({ key: "k", host: "" });
    expect(posthog.register).toHaveBeenCalledWith({
      client_type: "web",
      environment: "dev",
      event_schema_version: 2,
      is_demo: false,
    });
  });

  it("detects desktop when window.electron is present", async () => {
    vi.stubGlobal("window", { electron: {} });
    const { analytics, posthog } = await loadModule();
    analytics.initAnalytics({ key: "k", host: "" });
    expect(posthog.register).toHaveBeenCalledWith({
      client_type: "desktop",
      environment: "dev",
      event_schema_version: 2,
      is_demo: false,
    });
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
      environment: "dev",
      event_schema_version: 2,
      is_demo: false,
    });
  });

  it("is a no-op when analytics was never initialized", async () => {
    const { analytics, posthog } = await loadModule();
    analytics.resetAnalytics();
    expect(posthog.reset).not.toHaveBeenCalled();
    expect(posthog.register).not.toHaveBeenCalled();
  });
});

describe("normalizePageviewPath", () => {
  it("collapses resource-id segments to the section route", async () => {
    const { analytics } = await loadModule();
    expect(
      analytics.normalizePageviewPath("/acme/issues/8d5c1a2b-0035-4c62-9f14-1ad4215736a5"),
    ).toBe("/acme/issues");
    expect(analytics.normalizePageviewPath("/acme/issues/MUL-123")).toBe("/acme/issues");
    expect(
      analytics.normalizePageviewPath("/invite/8d5c1a2b-0035-4c62-9f14-1ad4215736a5"),
    ).toBe("/invite");
  });

  it("strips query string and hash", async () => {
    const { analytics } = await loadModule();
    expect(analytics.normalizePageviewPath("/acme/issues?status=open&view=board")).toBe(
      "/acme/issues",
    );
    expect(analytics.normalizePageviewPath("/acme/issues#section")).toBe("/acme/issues");
  });

  it("keeps non-id sub-sections and never drops the leading segment", async () => {
    const { analytics } = await loadModule();
    expect(analytics.normalizePageviewPath("/acme/settings/members")).toBe(
      "/acme/settings/members",
    );
    // A workspace slug that looks like an issue key must not be dropped.
    expect(analytics.normalizePageviewPath("/team-1/issues/MUL-9")).toBe("/team-1/issues");
    expect(analytics.normalizePageviewPath("/login")).toBe("/login");
    expect(analytics.normalizePageviewPath("/")).toBe("/");
  });
});

describe("capturePageview", () => {
  function captureMock(posthog: unknown) {
    return (posthog as { capture: ReturnType<typeof vi.fn> }).capture;
  }

  it("emits the section-normalized path as $current_url", async () => {
    const { analytics, posthog } = await loadModule();
    analytics.initAnalytics({ key: "k", host: "" });
    const capture = captureMock(posthog);
    capture.mockClear();

    analytics.capturePageview("/acme/issues/8d5c1a2b-0035-4c62-9f14-1ad4215736a5");

    expect(capture).toHaveBeenCalledTimes(1);
    expect(capture).toHaveBeenCalledWith("$pageview", { $current_url: "/acme/issues" });
  });

  it("dedupes consecutive views of the same section but fires on section change", async () => {
    const { analytics, posthog } = await loadModule();
    analytics.initAnalytics({ key: "k", host: "" });
    const capture = captureMock(posthog);
    capture.mockClear();

    // Two different issues collapse to the same section → one event.
    analytics.capturePageview("/acme/issues/a1b2c3d4-0035-4c62-9f14-1ad4215736a5");
    analytics.capturePageview("/acme/issues/b2c3d4e5-0035-4c62-9f14-1ad4215736a5");
    expect(capture).toHaveBeenCalledTimes(1);

    // A real section change fires again.
    analytics.capturePageview("/acme/projects");
    expect(capture).toHaveBeenCalledTimes(2);
  });

  it("re-emits the same section after resetAnalytics clears the dedup state", async () => {
    const { analytics, posthog } = await loadModule();
    analytics.initAnalytics({ key: "k", host: "" });
    const capture = captureMock(posthog);
    capture.mockClear();

    analytics.capturePageview("/acme/inbox");
    analytics.capturePageview("/acme/inbox");
    expect(capture).toHaveBeenCalledTimes(1);

    analytics.resetAnalytics();
    analytics.capturePageview("/acme/inbox");
    expect(capture).toHaveBeenCalledTimes(2);
  });
});

describe("captureException", () => {
  it("buffers a pre-init exception and flushes it on init", async () => {
    const { analytics, posthog } = await loadModule();
    const err = new Error("boom");

    // Before init: buffered, nothing sent yet.
    analytics.captureException(err, { source: "global-error" });
    expect(posthog.captureException).not.toHaveBeenCalled();

    // Init flushes the buffer in order.
    analytics.initAnalytics({ key: "k", host: "" });
    expect(posthog.captureException).toHaveBeenCalledTimes(1);
    expect(posthog.captureException).toHaveBeenCalledWith(
      err,
      expect.objectContaining({ source: "global-error" }),
    );
  });

  it("sends immediately once initialized", async () => {
    const { analytics, posthog } = await loadModule();
    analytics.initAnalytics({ key: "k", host: "" });
    posthog.captureException.mockClear();

    const err = new Error("later");
    analytics.captureException(err);
    expect(posthog.captureException).toHaveBeenCalledTimes(1);
    expect(posthog.captureException).toHaveBeenCalledWith(err, expect.any(Object));
  });
});

describe("before_send $exception pipeline", () => {
  // before_send is registered inside posthog.init's config; pull it back out of
  // the mock and drive it directly. Dedupe needs a working sessionStorage.
  function makeMemoryStorage() {
    const data = new Map<string, string>();
    return {
      getItem: (k: string) => (data.has(k) ? data.get(k)! : null),
      setItem: (k: string, v: string) => void data.set(k, v),
      removeItem: (k: string) => void data.delete(k),
      clear: () => data.clear(),
      key: (i: number) => Array.from(data.keys())[i] ?? null,
      get length() {
        return data.size;
      },
    };
  }

  type BeforeSend = (
    e: { event: string; properties: Record<string, unknown> } | null,
  ) => unknown;

  function getBeforeSend(posthog: { init: ReturnType<typeof vi.fn> }): BeforeSend {
    const config = posthog.init.mock.calls[0]?.[1] as { before_send: BeforeSend };
    return config.before_send;
  }

  function excEvent() {
    return {
      event: "$exception",
      properties: {
        $exception_list: [
          {
            type: "TypeError",
            value: "Bad email bob@corp.com",
            stacktrace: {
              frames: [{ filename: "a.tsx", function: "f", lineno: 1, colno: 2 }],
            },
          },
        ],
      },
    };
  }

  beforeEach(() => {
    vi.stubGlobal("sessionStorage", makeMemoryStorage());
  });

  it("redacts the message, then drops repeats past the per-fingerprint limit", async () => {
    const { analytics, posthog } = await loadModule();
    analytics.initAnalytics({ key: "k", host: "" });
    const beforeSend = getBeforeSend(posthog);

    const first = beforeSend(excEvent()) as { properties: { $exception_list: Array<{ value: string }> } };
    // Redaction still runs before the fuse.
    expect(first.properties.$exception_list[0]!.value).toBe("Bad email [redacted]");

    expect(beforeSend(excEvent())).not.toBeNull();
    expect(beforeSend(excEvent())).not.toBeNull();
    // 4th identical exception is dropped.
    expect(beforeSend(excEvent())).toBeNull();
  });

  it("passes non-$exception events through untouched", async () => {
    const { analytics, posthog } = await loadModule();
    analytics.initAnalytics({ key: "k", host: "" });
    const beforeSend = getBeforeSend(posthog);

    const evt = { event: "$pageview", properties: { $current_url: "/acme/issues" } };
    expect(beforeSend(evt)).toBe(evt);
  });
});
