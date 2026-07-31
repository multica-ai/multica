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
    stack: [
      { functionName: "parseMarkdownChunked", url: "assets/index-abc.js", lineNumber: 412, columnNumber: 17 },
      { functionName: "setContent", url: "assets/index-abc.js", lineNumber: 90, columnNumber: 4 },
    ],
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
  it("ships the bucketed route and the stack", () => {
    expect(buildFreezeEventProps(hang)).toEqual({
      source: "main-unresponsive",
      recovered: false,
      breadcrumb_ts: hang.ts,
      crashed_version: "0.4.11",
      path: "/:slug/issues",
      surface: "tab",
      stack: hang.context.stack,
      stack_depth: 2,
      // Top frame flattened so a hang groups by the blocking function without
      // unpacking the array.
      stack_function: "parseMarkdownChunked",
      stack_url: "assets/index-abc.js",
      stack_line: 412,
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

  // Review finding (MUL-5345): the flush side used to forward `context.stack`
  // wholesale. That array crosses an on-disk breadcrumb which is barely
  // validated on read, so an older build, a corrupt file or a future writer
  // could put a live handle in it and this would ship it.
  it("rebuilds stack frames instead of forwarding whatever the file held", () => {
    const props = buildFreezeEventProps({
      ...hang,
      context: {
        stack: [
          {
            functionName: "blockMainThread",
            url: "file:///Users/someone/Applications/Multica.app/out/renderer/main.js",
            location: { lineNumber: 12, columnNumber: 3 },
            scopeChain: [{ type: "local", object: { objectId: "{secret-scope}" } }],
            this: { objectId: "{secret-this}" },
            returnValue: "raw-value",
          },
        ],
      },
    });

    const serialized = JSON.stringify(props);
    expect(serialized).not.toContain("secret");
    expect(serialized).not.toContain("raw-value");
    // The absolute install path carries the OS username; only the tail ships.
    expect(serialized).not.toContain("someone");
    expect(props.stack).toEqual([
      {
        functionName: "blockMainThread",
        url: "renderer/main.js",
        lineNumber: 12,
        columnNumber: 3,
      },
    ]);
    expect(props.stack_function).toBe("blockMainThread");
    expect(props.stack_url).toBe("renderer/main.js");
    expect(props.stack_line).toBe(12);
  });

  it("omits the stack fields when the file held nothing usable", () => {
    for (const stack of [undefined, [], "not-an-array", [null]]) {
      const props = buildFreezeEventProps({ ...hang, context: { stack } });
      expect(props).not.toHaveProperty("stack");
      expect(props).not.toHaveProperty("stack_depth");
      expect(props).not.toHaveProperty("stack_function");
    }
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
      context: { ...hang.context, source: "spoofed", recovered: true },
    });

    expect(props.source).toBe("main-unresponsive");
    expect(props.recovered).toBe(false);
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
      recovered: false,
      breadcrumb_ts: hang.ts,
      crashed_version: "0.4.11",
    });
  });
});
