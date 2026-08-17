/**
 * Two regressions are pinned here, both found in review of MUL-5345.
 *
 * 1. The breadcrumb was acked inside `onCaptured`, which fires on hand-off to
 *    the PostHog SDK — not on delivery. An app that froze again or was killed
 *    while the request was still in flight lost the report AND the file, which
 *    is precisely the MUL-4115 failure the ack protocol exists to prevent.
 * 2. The whole breadcrumb context was spread into telemetry props, so the
 *    workspace slug, tab id and absolute window URL shipped with every report
 *    despite the stated "bucketed path only" constraint.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  buildFreezeEventProps,
  flushFreezeBreadcrumb,
  FREEZE_ACK_GRACE_MS,
} from "./freeze-flush";
import type { FreezeBreadcrumb } from "../../shared/freeze-breadcrumb";

const hang: FreezeBreadcrumb = {
  kind: "unresponsive",
  ts: 1_700_000_000_000,
  version: "0.4.11",
  context: {
    desktopRoute: {
      surface: "tab",
      path: "/:slug/issues",
      reportedAt: "2026-07-27T00:00:00.000Z",
    },
  },
};

function setup(overrides: Partial<FreezeBreadcrumb> | null = {}) {
  const ackFreeze = vi.fn();
  const capture = vi.fn();
  const breadcrumb = overrides === null ? null : { ...hang, ...overrides };
  const cleanup = flushFreezeBreadcrumb({
    getLastFreeze: () => breadcrumb,
    ackFreeze,
    capture,
  });
  const options = capture.mock.calls[0]?.[2] as
    | { onCaptured?: () => void; sendInstantly?: boolean }
    | undefined;
  return { ackFreeze, capture, cleanup, options };
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

describe("ack is not delivery", () => {
  it("does not retire the breadcrumb at hand-off", () => {
    const { ackFreeze, options } = setup();

    options?.onCaptured?.();

    expect(ackFreeze).not.toHaveBeenCalled();
  });

  it("retires it once the grace window has passed", () => {
    const { ackFreeze, options } = setup();
    options?.onCaptured?.();

    vi.advanceTimersByTime(FREEZE_ACK_GRACE_MS);

    expect(ackFreeze).toHaveBeenCalledWith(hang.ts);
  });

  it("keeps the breadcrumb when the app dies inside the grace window", () => {
    const { ackFreeze, options, cleanup } = setup();
    options?.onCaptured?.();

    vi.advanceTimersByTime(FREEZE_ACK_GRACE_MS - 1);
    // Second hang / force quit before the report ever left.
    cleanup();
    vi.advanceTimersByTime(FREEZE_ACK_GRACE_MS);

    expect(ackFreeze).not.toHaveBeenCalled();
  });

  it("never acks when the event was not captured at all (analytics disabled)", () => {
    const { ackFreeze } = setup();

    // onCaptured never fires on a build without an analytics key.
    vi.advanceTimersByTime(FREEZE_ACK_GRACE_MS * 10);

    expect(ackFreeze).not.toHaveBeenCalled();
  });

  it("sends instantly rather than waiting on the batch timer", () => {
    const { options } = setup();
    expect(options?.sendInstantly).toBe(true);
  });

  it("does nothing when there is no pending breadcrumb (the normal case)", () => {
    const { capture, ackFreeze } = setup(null);

    expect(capture).not.toHaveBeenCalled();
    expect(ackFreeze).not.toHaveBeenCalled();
  });

  it("reports a crash under its own event name", () => {
    const { capture } = setup({ kind: "render-process-gone" });
    expect(capture.mock.calls[0]?.[0]).toBe("client_crash");
  });
});

describe("telemetry props carry no raw identifiers", () => {
  it("ships the bucketed route and nothing else", () => {
    expect(buildFreezeEventProps(hang)).toEqual({
      source: "main-unresponsive",
      breadcrumb_ts: hang.ts,
      crashed_version: "0.4.11",
      path: "/:slug/issues",
      surface: "tab",
    });
  });

  it("drops workspace slug, tab id and window url if a context still carries them", () => {
    const leaky: FreezeBreadcrumb = {
      ...hang,
      context: {
        windowUrl: "file:///Users/someone/Applications/Multica.app/index.html",
        desktopRoute: {
          surface: "tab",
          path: "/:slug/issues",
          workspaceSlug: "acme",
          tabId: "tab-1",
          reportedAt: "2026-07-27T00:00:00.000Z",
        },
      },
    };

    const props = buildFreezeEventProps(leaky);

    const serialized = JSON.stringify(props);
    expect(serialized).not.toContain("acme");
    expect(serialized).not.toContain("tab-1");
    expect(serialized).not.toContain("someone");
    expect(props).not.toHaveProperty("workspaceSlug");
    expect(props).not.toHaveProperty("tabId");
    expect(props).not.toHaveProperty("windowUrl");
  });

  // MUL-5345: stack capture is gone, but a machine upgrading from v0.4.13–
  // v0.4.18 can still have a pending breadcrumb whose context holds a captured
  // stack — including the live handles the old capture path never shipped but
  // the on-disk file could carry. The whitelist is what keeps that off the
  // wire now that nothing sanitizes frames anymore.
  it("drops a stack left in a breadcrumb written by an older build", () => {
    const props = buildFreezeEventProps({
      ...hang,
      context: {
        ...hang.context,
        stack: [
          {
            functionName: "blockMainThread",
            url: "file:///Users/someone/Applications/Multica.app/out/renderer/main.js",
            location: { lineNumber: 12, columnNumber: 3 },
            scopeChain: [{ type: "local", object: { objectId: "{secret-scope}" } }],
          },
        ],
      },
    });

    const serialized = JSON.stringify(props);
    expect(serialized).not.toContain("secret");
    expect(serialized).not.toContain("someone");
    expect(serialized).not.toContain("blockMainThread");
    expect(props).not.toHaveProperty("stack");
    expect(props).not.toHaveProperty("stack_depth");
    expect(props).not.toHaveProperty("stack_function");
  });

  it("drops an unknown context key rather than forwarding it", () => {
    const props = buildFreezeEventProps({
      ...hang,
      context: { ...hang.context, somethingAddedLater: "raw-value" },
    });

    expect(JSON.stringify(props)).not.toContain("raw-value");
  });

  it("cannot have its own fields overridden by the context", () => {
    const props = buildFreezeEventProps({
      ...hang,
      context: { ...hang.context, source: "spoofed", breadcrumb_ts: 1 },
    });

    expect(props.source).toBe("main-unresponsive");
    expect(props.breadcrumb_ts).toBe(hang.ts);
  });

  it("keeps the crash reason as two scalars", () => {
    const props = buildFreezeEventProps({
      ...hang,
      kind: "render-process-gone",
      context: { details: { reason: "oom", exitCode: 5 } },
    });

    expect(props).toMatchObject({
      source: "render-process-gone",
      crash_reason: "oom",
      crash_exit_code: 5,
    });
  });

  it("survives a breadcrumb with no context at all", () => {
    const props = buildFreezeEventProps({ ...hang, context: {} });

    expect(props).toEqual({
      source: "main-unresponsive",
      breadcrumb_ts: hang.ts,
      crashed_version: "0.4.11",
    });
  });
});
