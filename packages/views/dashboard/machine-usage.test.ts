// @vitest-environment node

import { describe, expect, it } from "vitest";
import type { AgentRuntime, DashboardUsageByRuntime } from "@multica/core/types";
import { aggregateMachineUsage, RESTRICTED_RUNTIMES_ROW_ID } from "./machine-usage";

// One dollar of provider-reported cost per row keeps the sums exact.
const DOLLAR = 10_000_000_000;

function runtime(overrides: Partial<AgentRuntime>): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Claude (build-box)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: "user-1",
    visibility: "public",
    last_seen_at: null,
    created_at: "2026-05-17T11:00:00Z",
    updated_at: "2026-05-17T11:00:00Z",
    ...overrides,
  };
}

function row(runtimeId: string, tokens: number, taskCount: number, dollars = 1): DashboardUsageByRuntime {
  return {
    runtime_id: runtimeId,
    task_count: taskCount,
    usage: [
      {
        provider: "claude",
        model: "unpriced-test-model",
        input_tokens: tokens,
        output_tokens: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        cost_usd_ticks: dollars * DOLLAR,
        uncosted_input_tokens: 0,
        uncosted_output_tokens: 0,
        uncosted_cache_read_tokens: 0,
        uncosted_cache_write_tokens: 0,
      },
    ],
  };
}

const RUNTIMES = [
  // Two runtimes on one daemon: one machine, as on the Runtimes page.
  runtime({ id: "rt-claude", provider: "claude", name: "Claude (build-box)" }),
  runtime({ id: "rt-codex", provider: "codex", name: "Codex (build-box)" }),
  runtime({ id: "rt-laptop", daemon_id: "daemon-2", name: "Claude (laptop)", owner_id: "user-2" }),
];

describe("aggregateMachineUsage", () => {
  it("groups runtimes into machines and keeps each runtime's share", () => {
    const rows = aggregateMachineUsage(
      [row("rt-claude", 100, 2), row("rt-codex", 300, 1, 3), row("rt-laptop", 50, 4, 2)],
      RUNTIMES,
    );
    expect(rows.map((r) => [r.kind, r.tokens, r.cost, r.taskCount])).toEqual([
      ["machine", 400, 4, 3],
      ["machine", 50, 2, 4],
    ]);
    const [buildBox] = rows;
    if (buildBox?.kind !== "machine") throw new Error("expected a machine row");
    expect(buildBox.machine.title).toBe("build-box");
    // Most expensive runtime first.
    expect(buildBox.runtimes.map((r) => [r.runtime.id, r.tokens, r.taskCount])).toEqual([
      ["rt-codex", 300, 1],
      ["rt-claude", 100, 2],
    ]);
  });

  it("puts unknown runtimes in a deleted bucket and the server's bucket last", () => {
    const rows = aggregateMachineUsage(
      [
        row(RESTRICTED_RUNTIMES_ROW_ID, 1_000, 5, 10),
        row("rt-gone-1", 10, 1),
        row("rt-gone-2", 20, 1),
        row("rt-laptop", 5, 1),
      ],
      RUNTIMES,
    );
    expect(rows.map((r) => [r.kind, r.tokens, r.taskCount])).toEqual([
      ["machine", 5, 1],
      ["deleted", 30, 2],
      ["restricted", 1_000, 5],
    ]);
  });

  it("adds every input row to exactly one output row", () => {
    const input = [
      row("rt-claude", 7, 1),
      row("rt-codex", 11, 2),
      row("rt-gone", 13, 3),
      row(RESTRICTED_RUNTIMES_ROW_ID, 17, 4),
    ];
    const rows = aggregateMachineUsage(input, RUNTIMES);
    const sum = (key: "tokens" | "cost" | "taskCount") => rows.reduce((n, r) => n + r[key], 0);
    expect(sum("tokens")).toBe(48);
    expect(sum("cost")).toBe(4);
    expect(sum("taskCount")).toBe(10);
  });
});
