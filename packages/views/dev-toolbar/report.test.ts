// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { DevRecording } from "@multica/core/diagnostics";
import { buildReportMarkdown, reportCounts } from "./report";

const NOW = Date.UTC(2026, 10, 12, 10, 0, 0);

const recording: DevRecording = {
  requests: [
    { requestId: "rid-ok", method: "GET", path: "/api/issues", body: null, startedAt: NOW - 2_000, durationMs: 30, status: 200, error: null },
    { requestId: "rid-bad", method: "PATCH", path: "/api/issues/1", body: "{}", startedAt: NOW - 1_000, durationMs: 80, status: 422, error: "title too long" },
    { requestId: "rid-old", method: "GET", path: "/api/old", body: null, startedAt: NOW - 10 * 60_000, durationMs: 5, status: 500, error: "gone" },
  ],
  navigations: [{ at: NOW - 3_000, route: "/acme/issues", durationMs: 84 }],
  longTasks: [],
  errors: [
    {
      at: NOW - 500,
      source: "console",
      message: "Render failed ```",
      stack: "Error: Render failed\n    at Board (board.tsx:1:1)",
      route: "/acme/issues",
    },
  ],
  sync: [],
  memory: [],
  heapLimit: null,
};

const environment = {
  build: "web",
  apiUrl: "http://localhost:18080",
  route: "/acme/issues/1",
  workspaceSlug: "acme",
  role: "admin",
  userAgent: "test-agent",
  capturedAt: NOW,
};

describe("buildReportMarkdown", () => {
  it("bundles the description, screenshot, environment and recent history", () => {
    const markdown = buildReportMarkdown({
      description: "Card jumps back after drop",
      environment,
      recording,
      sections: { trail: true, errors: true, failedRequests: true },
      screenshotUrl: "https://files.test/shot.png",
    });

    expect(markdown).toMatch(/^Card jumps back after drop\n\n!\[Screenshot\]\(https:\/\/files\.test\/shot\.png\)/);
    expect(markdown).toContain("- Workspace: acme (admin)");
    expect(markdown).toContain("- API: localhost:18080");
    expect(markdown).toMatch(/- Captured: \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} UTC[+-]\d{2}:\d{2}/);
    expect(markdown).toContain("/acme/issues  (84ms)");
    expect(markdown).toContain("## Failed requests (1)");
    expect(markdown).toContain("PATCH /api/issues/1  → 422");
    expect(markdown).toContain("X-Request-ID: rid-bad");
    expect(markdown).not.toContain("/api/old");
    expect(markdown).toContain("at Board (board.tsx:1:1)");
    // A message containing a fence cannot close the block it sits in.
    expect(markdown).toContain("````text");
  });

  it("leaves out sections that were unchecked", () => {
    const markdown = buildReportMarkdown({
      description: "",
      environment,
      recording,
      sections: { trail: false, errors: false, failedRequests: false },
      screenshotUrl: null,
    });
    expect(markdown.startsWith("## Environment")).toBe(true);
    expect(markdown).not.toContain("## Errors");
    expect(markdown).not.toContain("Screenshot");
  });
});

describe("reportCounts", () => {
  it("counts only what falls inside the recording window", () => {
    expect(reportCounts(recording, NOW)).toEqual({ trail: 1, errors: 1, failedRequests: 1 });
  });
});
