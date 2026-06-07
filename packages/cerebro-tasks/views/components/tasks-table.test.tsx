// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types";
import type { CerebroTask, ColumnId } from "../../core/types";
import { TasksTable } from "./tasks-table";

const mockPush = vi.hoisted(() => vi.fn());
const transcriptClicks = vi.hoisted(() => vi.fn());

vi.mock("@multica/views/navigation", () => ({
  useNavigation: () => ({ push: mockPush }),
}));

vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: ({ name }: { name: string }) => (
    <span data-testid="actor-avatar">{name}</span>
  ),
}));

vi.mock("@multica/views/common/task-transcript", () => ({
  TranscriptButton: ({ task, title }: { task: AgentTask; title: string }) => (
    <button
      type="button"
      aria-label={title}
      data-task-id={task.id}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        transcriptClicks(task.id);
      }}
    >
      transcript
    </button>
  ),
}));

const visibleColumns: Record<ColumnId, boolean> = {
  agent: true,
  task: true,
  issue_name: true,
  issue_id: true,
  parent_issue: true,
  project: true,
  status: true,
  started: true,
  created_at: true,
  duration: true,
  cost: true,
  triggered_by: true,
};

function makeTask(overrides: Partial<CerebroTask> = {}): CerebroTask {
  return {
    task_id: "task-1",
    agent_id: "agent-1",
    agent_name: "Charlene",
    agent_avatar_url: undefined,
    issue_id: "issue-1",
    issue_number: 2985,
    issue_title: "Vis run-detaljer",
    parent_issue_id: undefined,
    parent_issue_number: undefined,
    parent_issue_title: undefined,
    parent_issue_assignee_name: undefined,
    project_id: "project-1",
    project_title: "Multica",
    project_lead_name: "Sara",
    chat_session_id: undefined,
    chat_title: undefined,
    task_title: "Run detail task",
    status: "completed",
    triggered_by_name: "Sara",
    created_at: "2026-06-05T19:00:00Z",
    dispatched_at: "2026-06-05T19:01:00Z",
    started_at: "2026-06-05T19:02:00Z",
    completed_at: "2026-06-05T19:03:00Z",
    cost_cents: 42,
    ...overrides,
  };
}

function renderTable(tasks: CerebroTask[]) {
  return render(
    <TasksTable
      tasks={tasks}
      isLoading={false}
      isError={false}
      workspaceSlug="firtal"
      visibleColumns={visibleColumns}
      groupBy="none"
    />,
  );
}

describe("TasksTable run details", () => {
  beforeEach(() => {
    mockPush.mockReset();
    transcriptClicks.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("shows run detail buttons for non-queued tasks only", () => {
    const { container } = renderTable([
      makeTask({ task_id: "completed-task", status: "completed" }),
      makeTask({ task_id: "queued-task", status: "queued" }),
    ]);

    const buttons = Array.from(container.querySelectorAll('button[aria-label="Vis run-detaljer"]'));
    expect(buttons).toHaveLength(2);
    expect(buttons.map((button) => button.getAttribute("data-task-id"))).toEqual([
      "completed-task",
      "completed-task",
    ]);
  });

  it("opens run details without triggering row navigation", () => {
    const { container } = renderTable([makeTask()]);

    const transcriptButton = container.querySelector('button[aria-label="Vis run-detaljer"]');
    expect(transcriptButton).not.toBeNull();
    fireEvent.click(transcriptButton!);

    expect(transcriptClicks).toHaveBeenCalledWith("task-1");
    expect(mockPush).not.toHaveBeenCalled();

    const taskTitle = screen.getAllByText("Run detail task")[0];
    expect(taskTitle).toBeDefined();
    fireEvent.click(taskTitle!);

    expect(mockPush).toHaveBeenCalledWith("/firtal/issues/issue-1");
  });
});
