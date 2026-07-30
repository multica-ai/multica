/**
 * MUL-5345 — a hang report has to say which page it happened on, and must not
 * say anything else about the user.
 *
 * The props used to be built by spreading the whole breadcrumb context, which
 * shipped the workspace slug, the tab id and the absolute window URL. Building
 * them by whitelist is what keeps that from coming back the next time the
 * context grows a field.
 */
import { describe, expect, it } from "vitest";

import { buildFreezeEventProps } from "./freeze-flush";
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

describe("buildFreezeEventProps", () => {
  it("reports the bucketed route at the top level, like the in-thread watchdog", () => {
    expect(buildFreezeEventProps(hang)).toEqual({
      source: "main-unresponsive",
      recovered: false,
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
    expect(buildFreezeEventProps({ ...hang, context: {} })).toEqual({
      source: "main-unresponsive",
      recovered: false,
      breadcrumb_ts: hang.ts,
      crashed_version: "0.4.11",
    });
  });
});
