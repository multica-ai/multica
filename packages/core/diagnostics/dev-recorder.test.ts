// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { getApiRequestObserver } from "../api/request-observer";
import { createLogger } from "../logger";
import {
  formatConsoleArgs,
  getDevRecording,
  recordableBody,
  redactPath,
  installDevRecorder,
  recordDevNavigationDuration,
  recordDevRoute,
  recordDevSyncStatus,
  resetDevRecorder,
  subscribeDevRecording,
} from "./dev-recorder";

describe("dev recorder", () => {
  let consoleError: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    vi.useFakeTimers();
    consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    resetDevRecorder();
    consoleError.mockRestore();
    vi.useRealTimers();
  });

  it("records a request from start to its response", () => {
    installDevRecorder();
    const observer = getApiRequestObserver()!;
    observer.onStart({ requestId: "r1", method: "POST", path: "/api/issues", body: '{"title":"x"}' });
    expect(getDevRecording().requests[0]).toMatchObject({ requestId: "r1", status: null, durationMs: null });

    vi.advanceTimersByTime(120);
    observer.onEnd({ requestId: "r1", status: 422, error: "title too long" });
    expect(getDevRecording().requests[0]).toMatchObject({ status: 422, error: "title too long", durationMs: 120 });
  });

  it("keeps credentials a request sent out of the recording", async () => {
    const fake = "fake-credential-for-test";
    const fetchMock = vi.fn().mockResolvedValue(new Response("{}", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    installDevRecorder();
    await new ApiClient("http://localhost:18080").connectVCS("ws-1", {
      provider: "gitlab",
      instance_url: "https://gitlab.example.test",
      access_token: fake,
    });

    expect(String(fetchMock.mock.calls[0]?.[1]?.body)).toContain(fake);
    const recorded = getDevRecording().requests[0]!;
    expect(recorded.body).not.toContain(fake);
    expect(JSON.parse(recorded.body!)).toMatchObject({ instance_url: "https://gitlab.example.test", access_token: "[redacted]" });
    vi.unstubAllGlobals();
  });

  it("expires history that has aged out of the window while idle, except requests still in flight", () => {
    installDevRecorder();
    const observer = getApiRequestObserver()!;
    recordDevRoute("/acme/issues");
    recordDevSyncStatus("connected");
    console.error("old error");
    observer.onStart({ requestId: "done", method: "GET", path: "/api/issues", body: null });
    observer.onEnd({ requestId: "done", status: 200, error: null });
    observer.onStart({ requestId: "hung", method: "GET", path: "/api/slow", body: null });

    vi.advanceTimersByTime(6 * 60_000);
    const recording = getDevRecording();
    expect(recording.requests.map((r) => r.requestId)).toEqual(["hung"]);
    expect([recording.errors.length, recording.navigations.length, recording.sync.length]).toEqual([0, 0, 0]);
  });

  it("tags errors with the route they happened on and keeps logging them", () => {
    installDevRecorder();
    recordDevRoute("/acme/issues");
    console.error("%s failed:", "Saving", new Error("boom"));

    expect(consoleError).toHaveBeenCalledOnce();
    expect(getDevRecording().errors[0]).toMatchObject({
      source: "console",
      message: "Saving failed: Error: boom",
      route: "/acme/issues",
    });
    expect(getDevRecording().errors[0]?.stack).toContain("boom");
  });

  it("leaves failed responses to the request log instead of counting them as errors too", () => {
    installDevRecorder();
    createLogger("api").error("← 500 /api/issues", { rid: "r1" });
    createLogger("auth").error("refresh failed");
    expect(getDevRecording().errors.map((e) => e.message)).toEqual([expect.stringContaining("refresh failed")]);
  });

  it("records uncaught errors but not Chromium's ResizeObserver notice", () => {
    installDevRecorder();
    window.dispatchEvent(new ErrorEvent("error", { message: "ResizeObserver loop completed with undelivered notifications." }));
    window.dispatchEvent(new ErrorEvent("error", { message: "boom", error: new TypeError("boom") }));
    expect(getDevRecording().errors.map((e) => [e.source, e.message])).toEqual([["uncaught", "TypeError: boom"]]);
  });

  it("records route changes and attaches the measured duration to the latest visit", () => {
    recordDevRoute("/acme/issues");
    recordDevRoute("/acme/inbox");
    recordDevRoute("/acme/inbox");
    recordDevNavigationDuration("/acme/inbox", 84);

    expect(getDevRecording().navigations.map((n) => [n.route, n.durationMs])).toEqual([
      ["/acme/issues", null],
      ["/acme/inbox", 84],
    ]);
  });

  it("records only actual sync status changes", () => {
    recordDevSyncStatus("connecting");
    recordDevSyncStatus("connected");
    recordDevSyncStatus("connected");
    expect(getDevRecording().sync.map((s) => s.status)).toEqual(["connecting", "connected"]);
  });

  it("notifies subscribers once per burst of changes", () => {
    const listener = vi.fn();
    subscribeDevRecording(listener);
    recordDevSyncStatus("connecting");
    recordDevSyncStatus("connected");
    expect(listener).not.toHaveBeenCalled();
    vi.advanceTimersByTime(100);
    expect(listener).toHaveBeenCalledOnce();
  });

  it("stops observing requests and restores console.error when reset", () => {
    const patched = console.error;
    installDevRecorder();
    expect(console.error).not.toBe(patched);
    resetDevRecorder();
    expect(console.error).toBe(patched);
    expect(getApiRequestObserver()).toBeNull();
  });

  it("removes its hooks and drops the recording when stopped, so a restart begins empty", () => {
    const patched = console.error;
    const stop = installDevRecorder();
    recordDevRoute("/acme/issues");
    console.error("before stop");
    expect(getDevRecording().errors).toHaveLength(1);

    stop();
    expect(console.error).toBe(patched);
    expect(getApiRequestObserver()).toBeNull();
    expect(vi.getTimerCount()).toBe(0);
    console.error("while stopped");
    window.dispatchEvent(new ErrorEvent("error", { message: "boom" }));
    expect(getDevRecording().errors).toEqual([]);
    expect(getDevRecording().navigations).toEqual([]);

    installDevRecorder();
    recordDevRoute("/acme/issues");
    expect(getDevRecording().navigations.map((n) => n.route)).toEqual(["/acme/issues"]);
    expect(getDevRecording().errors).toEqual([]);
  });

  it("leaves a running recording alone when a second install is stopped", () => {
    installDevRecorder();
    const stopSecond = installDevRecorder();
    stopSecond();
    console.error("still recording");
    expect(getDevRecording().errors).toHaveLength(1);
  });
});

describe("formatConsoleArgs", () => {
  it("substitutes format tokens and drops %c styles", () => {
    expect(formatConsoleArgs(["%cWarn%c %d rows", "color:red", "", 3]).message).toBe("Warn 3 rows");
  });

  it("serializes objects that follow the message", () => {
    expect(formatConsoleArgs(["bad payload", { id: 1 }]).message).toBe('bad payload {"id":1}');
  });
});

describe("recordableBody", () => {
  it("redacts credential fields at any depth but keeps usage counts", () => {
    const { body, bodyNote } = recordableBody(
      JSON.stringify({
        name: "slack",
        config: { bot_token: "xoxb-1", app_token: "xapp-1", signing_secret: "s" },
        custom_env: { OPENAI_API_KEY: "sk-1" },
        github_tokens: ["ghp-1"],
        input_tokens: 120,
      }),
    );
    expect(bodyNote).toBeNull();
    expect(JSON.parse(body!)).toEqual({
      name: "slack",
      config: { bot_token: "[redacted]", app_token: "[redacted]", signing_secret: "[redacted]" },
      custom_env: "[redacted]",
      github_tokens: "[redacted]",
      input_tokens: 120,
    });
  });

  it("marks a long body as cut instead of passing it off as the whole request", () => {
    const { body, bodyNote } = recordableBody(JSON.stringify({ content: "a".repeat(2100) }));
    expect(body!.length).toBe(2001);
    expect(bodyNote).toBe("truncated to 2000 of 2114 characters");
  });

  it("keeps no body it cannot inspect for secrets", () => {
    expect(recordableBody("token=abc")).toEqual({ body: null, bodyNote: "not JSON (9 characters), so not recorded" });
  });
});

describe("redactPath", () => {
  it("redacts credential query parameters only", () => {
    expect(redactPath("/api/x?token=abc&limit=5")).toBe("/api/x?token=%5Bredacted%5D&limit=5");
    expect(redactPath("/api/issues?limit=5")).toBe("/api/issues?limit=5");
  });
});
