import { describe, expect, it } from "vitest";
import { buildFreezeEventProps } from "./freeze-flush";
import type { FreezeBreadcrumb } from "../../shared/freeze-breadcrumb";

function breadcrumb(overrides: Partial<FreezeBreadcrumb> = {}): FreezeBreadcrumb {
  return {
    kind: "unresponsive",
    context: {},
    ts: 1_700_000_000_000,
    version: "0.3.30",
    ...overrides,
  };
}

describe("buildFreezeEventProps", () => {
  it("maps an unresponsive breadcrumb to a client_unresponsive event", () => {
    const { name, props } = buildFreezeEventProps(breadcrumb());
    expect(name).toBe("client_unresponsive");
    expect(props).toMatchObject({
      source: "main-unresponsive",
      recovered: false,
      breadcrumb_ts: 1_700_000_000_000,
      crashed_version: "0.3.30",
    });
  });

  it("maps a render-process-gone breadcrumb to a client_crash event with reason", () => {
    const { name, props } = buildFreezeEventProps(
      breadcrumb({ kind: "render-process-gone", context: { details: { reason: "oom" } } }),
    );
    expect(name).toBe("client_crash");
    expect(props.source).toBe("render-process-gone");
    expect(props.crash_reason).toBe("oom");
  });

  it("buckets the route to a template, dropping resource ids (P0②)", () => {
    const { props } = buildFreezeEventProps(
      breadcrumb({
        context: {
          desktopRoute: {
            surface: "tab",
            path: "/hazelkahlil/issues/019ed13f-5d22-78f3-ad92-76922fa96263",
            reportedAt: "2026-06-25T12:00:00.000Z",
          },
        },
      }),
    );
    expect(props.path).toBe("/hazelkahlil/issues");
  });

  it("keeps the asar window URL out of the route field", () => {
    const { props } = buildFreezeEventProps(
      breadcrumb({
        context: { windowUrl: "file:///Applications/Multica.app/.../index.html" },
      }),
    );
    expect(props.window_url).toBe("file:///Applications/Multica.app/.../index.html");
    expect(props.path).toBeUndefined();
  });

  it("redacts query tokens from CPU-profile script URLs before egress (P0①)", () => {
    const { props } = buildFreezeEventProps(
      breadcrumb({
        context: {
          cpuProfile: {
            nodes: [
              {
                id: 1,
                callFrame: {
                  functionName: "render",
                  url: "https://cdn.example/_next/chunk.js?token=supersecretvalue1234567890",
                  lineNumber: 1,
                  columnNumber: 2,
                },
                hitCount: 9,
              },
            ],
            startTime: 0,
            endTime: 1,
          },
        },
      }),
    );
    const profile = props.cpu_profile as { nodes: Array<{ callFrame: { url: string } }> };
    expect(profile.nodes[0].callFrame.url).toContain("[redacted]");
    expect(profile.nodes[0].callFrame.url).not.toContain("supersecretvalue");
    // The result is still valid structured data (functionName/line preserved).
    expect(profile.nodes[0].callFrame).toMatchObject({ functionName: "render", lineNumber: 1 });
  });
});
