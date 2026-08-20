// @vitest-environment node
/**
 * This context is attached to freeze/crash reports, which leave the machine.
 * The sanitizer is the last checkpoint before a renderer-supplied payload is
 * stored for that purpose, so it must construct the result explicitly rather
 * than pass through whatever it was handed — including from a renderer build
 * older than this one, which still sends `workspaceSlug` / `tabId`.
 */
import { describe, expect, it } from "vitest";

import { sanitizeRendererRouteContext } from "./renderer-route-context";

const reportedAt = new Date("2026-07-27T00:00:00.000Z");

describe("sanitizeRendererRouteContext", () => {
  it("keeps the bucketed route", () => {
    expect(
      sanitizeRendererRouteContext({ surface: "tab", path: "/:slug/issues" }, reportedAt),
    ).toEqual({
      surface: "tab",
      path: "/:slug/issues",
      reportedAt: "2026-07-27T00:00:00.000Z",
    });
  });

  it("drops raw identifiers a stale renderer still sends", () => {
    const sanitized = sanitizeRendererRouteContext(
      {
        surface: "tab",
        path: "/:slug/issues",
        workspaceSlug: "acme",
        tabId: "tab-1",
        issueId: "MUL-5345",
      },
      reportedAt,
    );

    expect(JSON.stringify(sanitized)).not.toContain("acme");
    expect(sanitized).not.toHaveProperty("workspaceSlug");
    expect(sanitized).not.toHaveProperty("tabId");
    expect(sanitized).not.toHaveProperty("issueId");
  });

  it("rejects a payload with an unknown surface", () => {
    expect(
      sanitizeRendererRouteContext({ surface: "browser", path: "/x" }, reportedAt),
    ).toBeNull();
  });

  it("rejects a payload with no usable path", () => {
    expect(
      sanitizeRendererRouteContext({ surface: "tab", path: "   " }, reportedAt),
    ).toBeNull();
    expect(sanitizeRendererRouteContext({ surface: "tab" }, reportedAt)).toBeNull();
  });

  it("rejects non-object input", () => {
    expect(sanitizeRendererRouteContext(null, reportedAt)).toBeNull();
    expect(sanitizeRendererRouteContext("tab", reportedAt)).toBeNull();
  });

  it("bounds string length so one payload can't bloat every report", () => {
    const sanitized = sanitizeRendererRouteContext(
      { surface: "tab", path: "/".padEnd(5_000, "x") },
      reportedAt,
    );

    expect(sanitized?.path.length).toBe(512);
  });
});
