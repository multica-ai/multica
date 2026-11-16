// @vitest-environment node
import { describe, expect, it } from "vitest";
import type {
  Attachment,
  ChatMessage,
  ChatPendingTask,
} from "@multica/core/types";
import { queuedChatMessages } from "./chat-queued-messages";

function userMessage(
  id: string,
  taskId: string | null,
  over: Partial<ChatMessage> = {},
): ChatMessage {
  return {
    id,
    chat_session_id: "session-1",
    role: "user",
    content: id,
    task_id: taskId,
    created_at: "2026-11-16T00:00:00Z",
    ...over,
  };
}

const screenshot = {
  id: "attachment-1",
  filename: "screenshot.png",
  content_type: "image/png",
} as Attachment;

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

    expect(queuedChatMessages(pending, messages).map((row) => row.content)).toEqual([
      "then update the docs",
      "and open a PR",
    ]);
  });

  it("carries the hidden message's attachments, including attachment-only sends", () => {
    const withFiles: ChatPendingTask = {
      ...pending,
      queued_tasks: [
        { ...pending.queued_tasks![0]! },
        {
          task_id: "task-file",
          status: "queued",
          created_at: "2026-11-16T00:00:04Z",
          message_id: "message-file",
        },
      ],
    };
    const messages = [
      userMessage("message-active", "task-active"),
      userMessage("message-next", "task-next", { attachments: [screenshot] }),
      userMessage("message-file", "task-file", {
        content: "",
        attachments: [screenshot],
      }),
    ];

    expect(queuedChatMessages(withFiles, messages)).toEqual([
      {
        task_id: "task-next",
        content: "then update the docs",
        attachments: [screenshot],
      },
      { task_id: "task-file", content: "", attachments: [screenshot] },
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

    expect(queuedChatMessages(withPlaceholder, [
      userMessage("optimistic-1", null),
    ])).toEqual([]);
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

    expect(queuedChatMessages(requeued, [
      userMessage("message-guidance", "task-active"),
    ])).toEqual([]);
  });

  it("returns nothing without anything to show", () => {
    expect(queuedChatMessages(undefined, [])).toEqual([]);
    expect(queuedChatMessages({
      task_id: "task-active",
      queued_tasks: [{ task_id: "task-next", status: "queued", created_at: "" }],
    }, [])).toEqual([]);
  });
});
