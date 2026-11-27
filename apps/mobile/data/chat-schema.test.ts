import { describe, expect, it } from "vitest";
import { ChatPendingTaskSchema, SendChatMessageResponseSchema } from "./schemas";

describe("ChatPendingTaskSchema", () => {
  it("keeps the pending head and valid queue rows when one row is malformed", () => {
    const parsed = ChatPendingTaskSchema.parse({
      task_id: "task-active",
      status: "running",
      queued_tasks: [
        {
          task_id: "task-next",
          status: "queued",
          created_at: "2026-08-03T00:00:00Z",
        },
        { status: "queued" },
      ],
    });

    expect(parsed.task_id).toBe("task-active");
    expect(parsed.queued_tasks).toEqual([
      {
        task_id: "task-next",
        status: "queued",
        created_at: "2026-08-03T00:00:00Z",
      },
    ]);
  });
});

describe("SendChatMessageResponseSchema", () => {
  const base = {
    message_id: "message-1",
    task_id: "task-1",
    created_at: "2026-08-05T00:00:00Z",
  };

  it("keeps queue position parity with web", () => {
    expect(SendChatMessageResponseSchema.parse({ ...base, queued: false }).queued).toBe(false);
  });

  it("ignores a malformed additive queue position", () => {
    expect(SendChatMessageResponseSchema.parse({ ...base, queued: "no" }).queued).toBeUndefined();
  });

  it("reads steering acceptance and the message's own input owner", () => {
    const parsed = SendChatMessageResponseSchema.parse({
      ...base,
      task_id: "task-retry",
      steered: true,
      message_task_id: "task-root",
      message_created_at: "2026-08-05T00:01:00Z",
    });
    expect(parsed).toMatchObject({
      steered: true,
      message_task_id: "task-root",
      message_created_at: "2026-08-05T00:01:00Z",
    });
  });

  it("degrades malformed steering fields to the legacy queued reading", () => {
    const parsed = SendChatMessageResponseSchema.parse({
      ...base,
      steered: "yes",
      message_task_id: "",
      message_created_at: 42,
    });
    expect(parsed.task_id).toBe("task-1");
    expect(parsed.steered).toBeUndefined();
    expect(parsed.message_task_id).toBeUndefined();
    expect(parsed.message_created_at).toBeUndefined();
  });
});
