// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getApiRequestObserver } from "../api/request-observer";
import { createLogger } from "../logger";
import {
  formatConsoleArgs,
  getDevRecording,
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
});

describe("formatConsoleArgs", () => {
  it("substitutes format tokens and drops %c styles", () => {
    expect(formatConsoleArgs(["%cWarn%c %d rows", "color:red", "", 3]).message).toBe("Warn 3 rows");
  });

  it("serializes objects that follow the message", () => {
    expect(formatConsoleArgs(["bad payload", { id: 1 }]).message).toBe('bad payload {"id":1}');
  });
});
