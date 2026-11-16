// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { ChatMessage, ChatPendingTask } from "@multica/core/types";
import { hideQueuedChatMessages } from "@multica/core/chat/pending";
import { queuedChatMessages } from "./chat-queued-messages";

function userMessage(id: string, taskId: string | null): ChatMessage {
  return {
    id,
    chat_session_id: "session-1",
    role: "user",
    content: id,
    task_id: taskId,
    created_at: "2026-11-16T00:00:00Z",
  };
}

const pending: ChatPendingTask = {
  task_id: "task-active",
  status: "running",
  supports_queue: true,
  queued_tasks: [
    {
      task_id: "task-next",
      status: "queued",
      created_at: "2026-11-16T00:00:02Z",
      message_id: "message-next",
      content: "then update the docs",
    },
    {
      task_id: "task-last",
      status: "queued",
      created_at: "2026-11-16T00:00:03Z",
      message_id: "message-last",
      content: "and open a PR",
    },
  ],
};

describe("queuedChatMessages", () => {
  it("shows the queued messages the transcript hides, in run order", () => {
    const messages = [
      userMessage("message-active", "task-active"),
      userMessage("message-next", "task-next"),
      userMessage("message-last", "task-last"),
    ];
    const visible = hideQueuedChatMessages(messages, pending);

    expect(visible.map((message) => message.id)).toEqual(["message-active"]);
    expect(queuedChatMessages(pending, visible).map((row) => row.content)).toEqual([
      "then update the docs",
      "and open a PR",
    ]);
  });

  it("skips a send still shown as its optimistic bubble", () => {
    const withPlaceholder: ChatPendingTask = {
      ...pending,
      queued_tasks: [{
        task_id: "optimistic-optimistic-1",
        status: "queued",
        created_at: "2026-11-16T00:00:04Z",
        message_id: "optimistic-1",
        content: "use the new path",
      }],
    };
    const visible = hideQueuedChatMessages(
      [userMessage("optimistic-1", null)],
      withPlaceholder,
    );

    expect(visible).toHaveLength(1);
    expect(queuedChatMessages(withPlaceholder, visible)).toEqual([]);
  });

  it("skips guidance the transcript still shows under its earlier owner", () => {
    // A re-queued steer moves to a new task; until messages refetch, the row
    // still carries the reply it joined and stays in the transcript.
    const requeued: ChatPendingTask = {
      ...pending,
      queued_tasks: [{
        task_id: "task-requeued",
        status: "queued",
        created_at: "2026-11-16T00:00:05Z",
        message_id: "message-guidance",
        content: "use the new path",
      }],
    };
    const visible = hideQueuedChatMessages(
      [userMessage("message-guidance", "task-active")],
      requeued,
    );

    expect(queuedChatMessages(requeued, visible)).toEqual([]);
  });

  it("returns nothing without queued content", () => {
    expect(queuedChatMessages(undefined, [])).toEqual([]);
    expect(queuedChatMessages({
      task_id: "task-active",
      queued_tasks: [{ task_id: "task-next", status: "queued", created_at: "" }],
    }, [])).toEqual([]);
  });
});
