import { describe, expect, it } from "vitest";
import { DEFAULT_TASKS_FILTER, type TasksFilter } from "./types";
import {
  parseTasksFilterFromSearchParams,
  serializeTasksFilter,
  tasksFilterPath,
} from "./url-state";

describe("tasks URL state", () => {
  it("serializes active filters into a shareable query string", () => {
    const filter: TasksFilter = {
      ...DEFAULT_TASKS_FILTER,
      agentId: "agent-1",
      issueId: "issue-1",
      projectId: "project-1",
      status: "running",
      type: "issue",
      range: "7d",
      search: "deploy",
      groupBy: "project",
      limit: 100,
      offset: 200,
    };

    expect(tasksFilterPath("/acme/tasks", filter)).toBe(
      "/acme/tasks?status=running&agent=agent-1&issue=issue-1&project=project-1&type=issue&range=7d&q=deploy&group=project&limit=100&offset=200",
    );
  });

  it("omits default values and trims empty search", () => {
    expect(serializeTasksFilter({ ...DEFAULT_TASKS_FILTER, search: "   " })).toBe("");
  });

  it("hydrates known filters and drops invalid enum/integer values", () => {
    const parsed = parseTasksFilterFromSearchParams(
      new URLSearchParams(
        "status=nope&type=chat&range=custom&from=2026-05-24T00%3A00%3A00.000Z&q=F%C3%A6tta&group=issue&limit=-1&offset=10",
      ),
    );

    expect(parsed).toMatchObject({
      status: null,
      type: "chat",
      range: "custom",
      customFrom: "2026-05-24T00:00:00.000Z",
      search: "Fætta",
      groupBy: "issue",
      limit: DEFAULT_TASKS_FILTER.limit,
      offset: 10,
    });
  });
});
