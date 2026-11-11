// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { parseWithFallback } from "@/lib/parse-response";
import {
  ProjectSchema,
  EMPTY_TASK_MESSAGE_LIST,
  TaskMessageListSchema,
} from "./schemas";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("project creator compatibility", () => {
  const project = {
    id: "project", workspace_id: "workspace", title: "Project", description: null,
    icon: null, status: "planned", priority: "none", lead_type: null, lead_id: null,
    created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
  };
  it.each([{}, { creator_type: null, creator_id: null }, { creator_type: 42, creator_id: {} }])("defaults missing or malformed creator fields: %j", (creator) => {
    const parsed = ProjectSchema.parse({ ...project, ...creator });
    expect(parsed.id).toBe("project");
    expect(parsed.creator_type).toBeNull();
    expect(parsed.creator_id).toBeNull();
  });
  it("preserves creator metadata", () => {
    expect(ProjectSchema.parse({ ...project, creator_type: "agent", creator_id: "agent" })).toMatchObject({ creator_type: "agent", creator_id: "agent" });
  });
});

describe("TaskMessageListSchema", () => {
  it("keeps an incomplete row renderable with safe defaults", () => {
    const parsed = TaskMessageListSchema.parse([
      {
        type: "future_event",
        task_id: 42,
        seq: "first",
        content: { malformed: true },
        input: "not-an-object",
      },
    ]);

    expect(parsed).toEqual([
      expect.objectContaining({
        type: "text",
        task_id: "",
        seq: 0,
        content: undefined,
        input: undefined,
      }),
    ]);
  });

  it("falls back to an empty transcript for a malformed response root", () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});

    expect(
      parseWithFallback(
        { messages: "not-an-array" },
        TaskMessageListSchema,
        EMPTY_TASK_MESSAGE_LIST,
        { endpoint: "GET /api/tasks/:id/messages" },
      ),
    ).toEqual([]);
  });
});
