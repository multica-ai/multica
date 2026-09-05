// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask, TaskUsage } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

import { IssueUsageDialog } from "./issue-usage-dialog";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-08-05T08:00:00Z",
    completed_at: "2026-08-05T08:11:00Z",
    result: null,
    error: null,
    created_at: "2026-08-05T08:00:00Z",
    trigger_summary: "Initial run",
    ...overrides,
  };
}

function usage(overrides: Partial<TaskUsage> = {}): TaskUsage {
  return {
    provider: "anthropic",
    model: "claude-opus-5",
    input_tokens: 1_000,
    output_tokens: 1_000,
    cache_read_tokens: 1_000,
    cache_write_tokens: 0,
    ...overrides,
  };
}

function open(tasks: AgentTask[]) {
  renderWithI18n(
    <IssueUsageDialog open onOpenChange={() => {}} identifier="ACM-1" tasks={tasks} />,
  );
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
});

afterEach(cleanup);

describe("IssueUsageDialog", () => {
  it("floors the cache hit rate instead of rounding it up to 100%", () => {
    // 99.55% — rounding would print "100% hit rate" and claim every token came
    // from cache on an issue that plainly read some fresh input.
    open([
      makeTask({
        usage: [usage({ input_tokens: 573_500, cache_read_tokens: 126_000_000 })],
      }),
    ]);

    expect(screen.getByText(/99% hit rate/)).toBeInTheDocument();
    expect(screen.queryByText(/100% hit rate/)).not.toBeInTheDocument();
  });

  it("still says 100% when the hit rate really is 100%", () => {
    open([
      makeTask({ usage: [usage({ input_tokens: 0, cache_read_tokens: 1_000 })] }),
    ]);

    expect(screen.getByText(/100% hit rate/)).toBeInTheDocument();
  });

  it("keeps the full model list reachable when the cell truncates", () => {
    // A run that spilled across models carries ids long enough to set the
    // table's width on their own; the cell is capped, so the untruncated list
    // has to survive somewhere.
    const models = "claude-haiku-4-5-20251001, claude-opus-5[1m]";
    open([
      makeTask({
        usage: [
          usage({ model: "claude-haiku-4-5-20251001" }),
          usage({ model: "claude-opus-5[1m]" }),
        ],
      }),
    ]);

    expect(screen.getByTitle(models)).toBeInTheDocument();
  });

  it("gives every terminal run a status a screen reader can read", () => {
    // The status glyph is aria-hidden, so without the paired label a failed
    // run and a completed one are indistinguishable to assistive tech.
    open([
      makeTask({ id: "t-ok", usage: [usage()] }),
      makeTask({ id: "t-bad", status: "failed", usage: [usage()] }),
    ]);

    expect(screen.getByText("Completed")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
  });
});
