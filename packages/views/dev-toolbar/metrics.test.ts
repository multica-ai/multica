// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { DevRealtimeEvent, DevRequest } from "@multica/core/diagnostics";
import type { AgentTask } from "@multica/core/types";
import {
  activeTasks,
  apiHost,
  blockedRatio,
  cacheEffectSummary,
  curlCommand,
  eventMatches,
  eventsInLastMinute,
  firstStackFrame,
  formatAge,
  formatBytes,
  issueIdsForKey,
  jankTone,
  memoryTone,
  navDuration,
  navTone,
  percentile,
  requestFailed,
} from "./metrics";

function event(overrides: Partial<DevRealtimeEvent>): DevRealtimeEvent {
  return {
    id: 1,
    at: 0,
    kind: "frame",
    type: "issue:updated",
    issueId: null,
    issueKey: null,
    payload: null,
    payloadNote: null,
    actor: null,
    workspaceId: "ws-1",
    cache: { written: [], invalidated: [], removed: [], refetched: [] },
    downMs: null,
    ...overrides,
  };
}

const MB = 1024 * 1024;

describe("formatBytes", () => {
  it("shows megabytes below a gigabyte and gigabytes above", () => {
    expect(formatBytes(152.4 * MB)).toBe("152MB");
    expect(formatBytes(1536 * MB)).toBe("1.50GB");
  });
});

describe("memoryTone", () => {
  it("warns from 500MB and flags from 1GB", () => {
    expect(memoryTone(499 * MB)).toBe("normal");
    expect(memoryTone(500 * MB)).toBe("warn");
    expect(memoryTone(1024 * MB)).toBe("bad");
  });
});

describe("navDuration", () => {
  it("measures from the triggering input to the painted frame", () => {
    expect(navDuration(1_000, 1_542)).toBe(542);
  });

  it("ignores route changes no recent input explains", () => {
    expect(navDuration(null, 5_000)).toBeNull();
    expect(navDuration(1_000, 12_000)).toBeNull();
    expect(navDuration(2_000, 1_000)).toBeNull();
  });
});

describe("navTone", () => {
  it("warns from 300ms and flags from 1s", () => {
    expect(navTone(299)).toBe("normal");
    expect(navTone(300)).toBe("warn");
    expect(navTone(1_000)).toBe("bad");
  });
});

describe("blockedRatio", () => {
  it("counts only the part of each task inside the window", () => {
    const tasks = [
      { start: 0, end: 2_000 },
      { start: 9_000, end: 9_500 },
      { start: 19_000, end: 19_500 },
    ];
    // Window is [10_000, 20_000]: only the last task overlaps it.
    expect(blockedRatio(tasks, 20_000, 10_000)).toBeCloseTo(0.05);
    // Window is [1_000, 11_000]: 1_000ms of the first task plus the second.
    expect(blockedRatio(tasks, 11_000, 10_000)).toBeCloseTo(0.15);
  });

  it("is zero with no long tasks", () => {
    expect(blockedRatio([], 20_000)).toBe(0);
  });
});

describe("jankTone", () => {
  it("warns from 5% and flags from 20%", () => {
    expect(jankTone(0.049)).toBe("normal");
    expect(jankTone(0.05)).toBe("warn");
    expect(jankTone(0.2)).toBe("bad");
  });
});

describe("apiHost", () => {
  it("keeps host and port, and passes through what is not a URL", () => {
    expect(apiHost("http://localhost:18080")).toBe("localhost:18080");
    expect(apiHost("https://api.multica.ai/")).toBe("api.multica.ai");
    expect(apiHost("same-origin")).toBe("same-origin");
  });
});

function request(overrides: Partial<DevRequest>): DevRequest {
  return {
    requestId: "rid-1",
    method: "GET",
    path: "/api/issues",
    body: null,
    bodyNote: null,
    startedAt: 0,
    durationMs: 40,
    status: 200,
    error: null,
    ...overrides,
  };
}

describe("requestFailed", () => {
  it("fails error statuses and lost responses, but not in-flight or cancelled requests", () => {
    expect(requestFailed(request({ status: 500 }))).toBe(true);
    expect(requestFailed(request({ status: null, error: "TypeError: Failed to fetch" }))).toBe(true);
    expect(requestFailed(request({ status: 204 }))).toBe(false);
    expect(requestFailed(request({ status: null, durationMs: null }))).toBe(false);
    expect(requestFailed(request({ status: null, error: "aborted" }))).toBe(false);
  });
});

describe("percentile", () => {
  it("uses the nearest rank", () => {
    const values = Array.from({ length: 20 }, (_, i) => i + 1);
    expect(percentile(values, 95)).toBe(19);
    expect(percentile(values, 50)).toBe(10);
    expect(percentile([7], 95)).toBe(7);
    expect(percentile([], 95)).toBeNull();
  });
});

describe("curlCommand", () => {
  it("replays the request without embedding a credential", () => {
    const command = curlCommand(
      "http://localhost:18080/",
      request({ method: "POST", body: `{"title":"it's"}` }),
      "acme",
    );
    expect(command).toBe(
      [
        "curl -X POST 'http://localhost:18080/api/issues'",
        `-H "Authorization: Bearer $MULTICA_TOKEN"`,
        "-H 'X-Workspace-Slug: acme'",
        "-H 'Content-Type: application/json'",
        `--data '{"title":"it'\\''s"}'`,
      ].join(" \\\n  "),
    );
  });
});

describe("curlCommand with an incomplete body", () => {
  it("refuses to build a command that would send a different request", () => {
    const truncated = request({ method: "POST", body: '{"content":"aaa…', bodyNote: "truncated to 2000 of 2113 characters" });
    expect(curlCommand("http://localhost:18080", truncated, "acme")).toBeNull();
  });
});

describe("firstStackFrame", () => {
  it("skips the message line", () => {
    expect(firstStackFrame("Error: boom\n    at save (issue.tsx:12:3)\n    at run")).toBe("at save (issue.tsx:12:3)");
    expect(firstStackFrame(null)).toBeNull();
  });
});

describe("eventsInLastMinute", () => {
  it("sums the per-second frame counts from the last minute only", () => {
    const counts = [
      { at: 0, count: 7 },
      { at: 50_000, count: 400 },
      { at: 60_000, count: 101 },
    ];
    expect(eventsInLastMinute(counts, 61_000)).toBe(501);
  });
});

describe("cacheEffectSummary", () => {
  it("lists each kind of change with its count", () => {
    expect(cacheEffectSummary({ written: ["a"], invalidated: ["b", "c"], removed: [], refetched: ["b"] })).toBe(
      "1 written · 2 invalidated · 1 refetched",
    );
    expect(cacheEffectSummary(event({}).cache)).toBe("");
  });
});

describe("issueIdsForKey", () => {
  it("resolves an issue key through events and cached issues that carry both", () => {
    const events = [event({ issueId: "issue-1", issueKey: "MUL-1" })];
    const cached = [{ id: "issue-9", identifier: "mul-1" }, { id: "issue-2", identifier: "MUL-2" }, null, []];
    expect(issueIdsForKey(" mul-1 ", events, cached)).toEqual(["issue-1", "issue-9"]);
  });

  it("does nothing for a search that is not an issue key", () => {
    expect(issueIdsForKey("issue:updated", [event({ issueId: "issue-1", issueKey: "MUL-1" })], [])).toEqual([]);
  });
});

describe("eventMatches", () => {
  const comment = event({ type: "comment:created", issueId: "issue-1", payload: '{"comment":{"body":"Looks good"}}' });

  it("matches type, issue ID and payload text, case-insensitively", () => {
    expect(eventMatches(comment, "COMMENT:", [])).toBe(true);
    expect(eventMatches(comment, "issue-1", [])).toBe(true);
    expect(eventMatches(comment, "looks good", [])).toBe(true);
    expect(eventMatches(comment, "MUL-1", [])).toBe(false);
  });

  it("matches an issue key through the IDs it resolved to", () => {
    expect(eventMatches(comment, "MUL-1", ["issue-1"])).toBe(true);
  });
});

describe("activeTasks", () => {
  it("keeps tasks that have not finished, oldest first", () => {
    const task = (id: string, status: AgentTask["status"], created_at: string) =>
      ({ id, status, created_at }) as AgentTask;
    const tasks = [
      task("done", "completed", "2026-01-01T00:00:00Z"),
      task("later", "running", "2026-01-01T00:02:00Z"),
      task("first", "queued", "2026-01-01T00:01:00Z"),
      task("parked", "waiting_local_directory", "2026-01-01T00:03:00Z"),
    ];
    expect(activeTasks(tasks).map((t) => t.id)).toEqual(["first", "later", "parked"]);
  });
});

describe("formatAge", () => {
  it("rounds down to the largest whole unit", () => {
    expect([formatAge(400), formatAge(12_900), formatAge(4 * 60_000 + 5), formatAge(2 * 3_600_000)]).toEqual([
      "now",
      "12s",
      "4m",
      "2h",
    ]);
  });
});
