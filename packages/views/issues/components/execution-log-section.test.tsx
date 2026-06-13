// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  taskMessagesOptions: vi.fn(),
}));

vi.mock("@multica/core/chat/queries", () => ({
  taskMessagesOptions: mockState.taskMessagesOptions,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

vi.mock("../../common/task-transcript", () => ({
  TranscriptButton: ({ title }: { title?: string }) => (
    <button type="button">{title ?? "Transcript"}</button>
  ),
}));

vi.mock("./terminate-task-confirm-dialog", () => ({
  TerminateTaskConfirmDialog: () => null,
}));

import { ActiveTaskRow } from "./execution-log-section";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-06-08T08:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-06-08T08:00:00Z",
    trigger_summary: "Started from comment",
    ...overrides,
  };
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-06-08T08:05:04Z"));
});

afterEach(() => {
  vi.useRealTimers();
});

describe("ActiveTaskRow", () => {
  it("renders running status as elapsed time only", () => {
    renderWithI18n(<ActiveTaskRow task={makeTask()} issueId="issue-1" />);

    expect(screen.getByText("5m 04s")).toBeInTheDocument();
    expect(screen.queryByText(/events?/i)).not.toBeInTheDocument();
    expect(screen.getByText("Started from comment")).toBeInTheDocument();
    expect(screen.getByText("View transcript")).toBeInTheDocument();
    expect(mockState.taskMessagesOptions).not.toHaveBeenCalled();
  });
});
