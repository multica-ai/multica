// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { parseWithFallback } from "@/lib/parse-response";
import {
  EMPTY_TASK_MESSAGE_LIST,
  TaskMessageListSchema,
} from "./schemas";

afterEach(() => {
  vi.restoreAllMocks();
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
