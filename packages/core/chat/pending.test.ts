import { describe, expect, it } from "vitest";
import {
  enqueuePendingChatTask,
  hideQueuedChatMessages,
  prioritizePendingChatTask,
  promotePendingChatTask,
  removePendingChatTask,
} from "./pending";

const task = (task_id: string, created_at: string) => ({
  task_id,
  status: "queued",
  created_at,
  content: `message ${task_id}`,
});

describe("pending chat queue", () => {
  it("keeps the active task at the head and follow-ups in FIFO order", () => {
    const active = { ...task("active", "2026-01-01T00:00:00Z"), status: "running" };
    const withLater = enqueuePendingChatTask(active, task("later", "2026-01-01T00:00:02Z"));
    const result = enqueuePendingChatTask(withLater, task("next", "2026-01-01T00:00:01Z"));

    expect(result.task_id).toBe("active");
    expect(result.queued_tasks?.map((item) => item.task_id)).toEqual(["next", "later"]);
  });

  it("treats the first accepted task as the current head before it starts running", () => {
    const queued = {
      ...task("next", "2026-01-01T00:00:01Z"),
      message_id: "message-next",
    };
    const pending = enqueuePendingChatTask(undefined, queued);

    expect(pending.queued_tasks).toEqual([]);
    expect(promotePendingChatTask(pending, "next", "running").queued_tasks).toEqual([]);
  });

  it("keeps an authoritative queued send hidden while its head is still loading", () => {
    const queued = {
      ...task("next", "2026-01-01T00:00:01Z"),
      message_id: "message-next",
    };

    expect(enqueuePendingChatTask(undefined, queued, true)).toEqual({
      queued_tasks: [queued],
    });
  });

  it("preserves a follow-up when its response arrives before the head response", () => {
    const followUp = {
      ...task("next", "2026-01-01T00:00:01Z"),
      message_id: "message-next",
    };
    const waitingForHead = enqueuePendingChatTask(undefined, followUp, true);
    const head = {
      ...task("head", "2026-01-01T00:00:00Z"),
      message_id: "message-head",
    };

    expect(enqueuePendingChatTask(waitingForHead, head, false)).toEqual({
      ...head,
      queued_tasks: [followUp],
    });
  });

  it("keeps a send response preview when a sparse queued event arrives later", () => {
    const current = {
      ...task("active", "2026-01-01T00:00:00Z"),
      status: "running",
      queued_tasks: [
        {
          ...task("next", "2026-01-01T00:00:01Z"),
          message_id: "message-next",
          content: "Keep this visible preview",
        },
      ],
    };

    const result = enqueuePendingChatTask(current, {
      task_id: "next",
      status: "queued",
      created_at: "2026-01-01T00:00:03Z",
    });

    expect(result.queued_tasks).toEqual([
      expect.objectContaining({
        task_id: "next",
        created_at: "2026-01-01T00:00:01Z",
        message_id: "message-next",
        content: "Keep this visible preview",
      }),
    ]);
  });

  it("enriches a sparse queued event when the send response arrives later", () => {
    const current = {
      ...task("active", "2026-01-01T00:00:00Z"),
      status: "running",
      queued_tasks: [{
        task_id: "next",
        status: "queued",
        created_at: "2026-01-01T00:00:01Z",
      }],
    };

    const result = enqueuePendingChatTask(current, {
      ...task("next", "2026-01-01T00:00:01Z"),
      message_id: "message-next",
      content: "Visible accepted prompt",
    });

    expect(result.queued_tasks).toEqual([
      expect.objectContaining({
        task_id: "next",
        message_id: "message-next",
        content: "Visible accepted prompt",
      }),
    ]);
  });

  it("does not downgrade a dispatched task when its send response arrives later", () => {
    const current = {
      ...task("first", "2026-01-01T00:00:00Z"),
      status: "dispatched",
      queued_tasks: [],
    };

    const result = enqueuePendingChatTask(current, {
      ...task("first", "2026-01-01T00:00:00Z"),
      message_id: "message-first",
      content: "Keep the accepted prompt",
    });

    expect(result).toEqual(expect.objectContaining({
      task_id: "first",
      status: "dispatched",
      message_id: "message-first",
      content: "Keep the accepted prompt",
      queued_tasks: [],
    }));
  });

  it("promotes a queued task without losing later work", () => {
    const current = {
      ...task("active", "2026-01-01T00:00:00Z"),
      status: "running",
      supports_queue: true,
      queued_tasks: [
        task("next", "2026-01-01T00:00:01Z"),
        task("later", "2026-01-01T00:00:02Z"),
      ],
    };

    const result = promotePendingChatTask(current, "next", "running");
    expect(result.task_id).toBe("next");
    expect(result.status).toBe("running");
    expect(result.supports_queue).toBe(true);
    expect(result.queued_tasks?.map((item) => item.task_id)).toEqual(["later"]);
  });

  it("ignores a stale dispatch for an unknown task while another task is active", () => {
    const current = {
      ...task("active", "2026-01-01T00:00:00Z"),
      status: "running",
      queued_tasks: [task("next", "2026-01-01T00:00:01Z")],
    };

    expect(promotePendingChatTask(current, "stale", "running")).toBe(current);
  });

  it("does not resurrect an unknown task after the pending query cleared", () => {
    expect(promotePendingChatTask(undefined, "stale", "running")).toEqual({});
    expect(promotePendingChatTask({ supports_queue: true }, "stale", "running")).toEqual({
      supports_queue: true,
    });
  });

  it("removes only the selected queued task", () => {
    const current = {
      ...task("active", "2026-01-01T00:00:00Z"),
      status: "running",
      queued_tasks: [task("next", "2026-01-01T00:00:01Z")],
    };

    expect(removePendingChatTask(current, "next")).toEqual({
      ...current,
      queued_tasks: [],
    });
  });

  it("promotes the first queued task when the active task finishes", () => {
    const current = {
      ...task("active", "2026-01-01T00:00:00Z"),
      status: "running",
      supports_queue: true,
      queued_tasks: [
        task("next", "2026-01-01T00:00:01Z"),
        task("later", "2026-01-01T00:00:02Z"),
      ],
    };

    const result = removePendingChatTask(current, "active");
    expect(result.task_id).toBe("next");
    expect(result.supports_queue).toBe(true);
    expect(result.queued_tasks?.map((item) => item.task_id)).toEqual(["later"]);
  });

  it("keeps queued prompts out of the settled transcript", () => {
    const messages = [
      {
        id: "message-active",
        chat_session_id: "session",
        role: "user" as const,
        content: "active",
        task_id: "active",
        created_at: "2026-01-01T00:00:00Z",
      },
      {
        id: "message-next",
        chat_session_id: "session",
        role: "user" as const,
        content: "next",
        task_id: "next",
        created_at: "2026-01-01T00:00:01Z",
      },
    ];
    const pending = {
      ...task("active", "2026-01-01T00:00:00Z"),
      status: "running",
      queued_tasks: [{ ...task("next", "2026-01-01T00:00:01Z"), message_id: "message-next" }],
    };

    expect(hideQueuedChatMessages(messages, pending)).toEqual([messages[0]]);
    const waiting = removePendingChatTask(pending, "active");
    expect(hideQueuedChatMessages(messages, waiting)).toEqual(messages);
    expect(
      hideQueuedChatMessages(messages, promotePendingChatTask(waiting, "next", "running")),
    ).toEqual(
      messages,
    );
  });

  it("keeps a queued retry's historical root prompt in the transcript", () => {
    const root = {
      id: "message-root",
      chat_session_id: "session",
      role: "user" as const,
      content: "retry this",
      task_id: "task-root",
      created_at: "2026-01-01T00:00:00Z",
    };
    const pending = {
      ...task("task-retry", "2026-01-01T00:00:01Z"),
      queued_tasks: [{
        ...task("task-retry", "2026-01-01T00:00:01Z"),
        message_id: root.id,
      }],
    };

    expect(hideQueuedChatMessages([root], pending)).toEqual([root]);
  });

  it("moves a send-now task to the front without disturbing the remaining queue", () => {
    const current = {
      ...task("active", "2026-01-01T00:00:00Z"),
      status: "running",
      queued_tasks: [
        task("next", "2026-01-01T00:00:01Z"),
        task("send-now", "2026-01-01T00:00:02Z"),
        task("later", "2026-01-01T00:00:03Z"),
      ],
    };

    expect(
      prioritizePendingChatTask(current, "send-now").queued_tasks?.map((item) => item.task_id),
    ).toEqual(["send-now", "next", "later"]);
  });

  it("preserves send-now order when the active task is removed", () => {
    const current = prioritizePendingChatTask({
      ...task("active", "2026-01-01T00:00:00Z"),
      status: "running",
      queued_tasks: [
        task("next", "2026-01-01T00:00:01Z"),
        task("send-now", "2026-01-01T00:00:02Z"),
      ],
    }, "send-now");

    const result = removePendingChatTask(current, "active");
    expect(result.task_id).toBe("send-now");
    expect(result.queued_tasks?.map((item) => item.task_id)).toEqual(["next"]);
  });
});

describe("promotePendingChatTask wait_reason", () => {
  const waiting = "waiting_local_directory";

  it("keeps the reason a hold arrives with", () => {
    const promoted = promotePendingChatTask(
      { task_id: "t1", status: "dispatched" },
      "t1",
      waiting,
      undefined,
      "NuvioTV (held by task a1b2c3d4)",
    );
    expect(promoted.status).toBe(waiting);
    expect(promoted.wait_reason).toBe("NuvioTV (held by task a1b2c3d4)");
  });

  it("carries the cached reason across repeat events for the same hold", () => {
    // The daemon stamps wait_reason once; a later event for the same parked
    // task may arrive without it, and the pill must not blank mid-wait.
    const promoted = promotePendingChatTask(
      { task_id: "t1", status: waiting, wait_reason: "NuvioTV" },
      "t1",
      waiting,
    );
    expect(promoted.wait_reason).toBe("NuvioTV");
  });

  it("drops the reason the moment the task stops waiting", () => {
    // The server never clears the column, so a task that has been running for
    // ten minutes would otherwise still claim to be held by another task.
    const promoted = promotePendingChatTask(
      { task_id: "t1", status: waiting, wait_reason: "NuvioTV" },
      "t1",
      "running",
    );
    expect(promoted.status).toBe("running");
    expect(promoted.wait_reason).toBeUndefined();
  });

  it("does not inherit a reason when a queued follow-up is promoted", () => {
    const promoted = promotePendingChatTask(
      {
        task_id: "t1",
        status: waiting,
        wait_reason: "NuvioTV",
        queued_tasks: [{ task_id: "t2", status: "queued", created_at: "2026-07-01T00:00:00Z" }],
      },
      "t2",
      "running",
    );
    expect(promoted.task_id).toBe("t2");
    expect(promoted.wait_reason).toBeUndefined();
  });
});
