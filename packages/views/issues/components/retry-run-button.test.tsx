// @vitest-environment jsdom

import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const rerunIssue = vi.fn(async () => ({ id: "new-task" }));

vi.mock("@multica/core/api", () => ({
  api: { rerunIssue: (...args: [string, string, boolean]) => rerunIssue(...args) },
  dispatchReasonCode: () => undefined,
}));

import { prefersFreshRetry, RetryRunButton } from "./retry-run-button";

afterEach(() => {
  cleanup();
  rerunIssue.mockClear();
});

function task(failureReason: string): AgentTask {
  return {
    id: "task-1",
    status: "failed",
    failure_reason: failureReason,
  } as AgentTask;
}

async function openMenu(): Promise<[HTMLElement, HTMLElement]> {
  fireEvent.click(screen.getByRole("button", { name: "Retry run" }));
  const items = await screen.findAllByRole("menuitem");
  expect(items).toHaveLength(2);
  return [items[0], items[1]];
}

describe("RetryRunButton", () => {
  it("offers both options, resuming by default for an ordinary failure", async () => {
    renderWithI18n(<RetryRunButton task={task("agent_error")} issueId="issue-1" />);
    const items = await openMenu();

    expect(items.map((el) => el.textContent)).toEqual([
      "Continue the previous session",
      "Start fresh",
    ]);

    fireEvent.click(items[0]);
    expect(rerunIssue).toHaveBeenCalledWith("issue-1", "task-1", false);
  });

  it("recommends a fresh start when the run died spinning", async () => {
    renderWithI18n(<RetryRunButton task={task("idle_watchdog")} issueId="issue-1" />);
    const items = await openMenu();

    expect(items.map((el) => el.textContent)).toEqual([
      "Start fresh",
      "Continue the previous session",
    ]);

    fireEvent.click(items[0]);
    expect(rerunIssue).toHaveBeenCalledWith("issue-1", "task-1", true);
  });

  it("still lets a spinning run resume its session when asked", async () => {
    renderWithI18n(<RetryRunButton task={task("timeout")} issueId="issue-1" />);
    const items = await openMenu();
    fireEvent.click(items[1]);
    expect(rerunIssue).toHaveBeenCalledWith("issue-1", "task-1", false);
  });
});

describe("prefersFreshRetry", () => {
  it("only flags runs whose session is known to be poisoned", () => {
    for (const reason of ["timeout", "idle_watchdog", "codex_semantic_inactivity", "agent_error.context_overflow"]) {
      expect(prefersFreshRetry(task(reason))).toBe(true);
    }
    for (const reason of ["agent_error", "runtime_offline", "manual", ""]) {
      expect(prefersFreshRetry(task(reason))).toBe(false);
    }
  });
});
