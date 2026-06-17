import { describe, expect, it } from "vitest";

import { isTaskMessageTaskId, shouldPollPendingChatTask, taskMessagesOptions } from "./queries";

describe("taskMessagesOptions", () => {
  it("fetches task messages for persisted UUID task ids", () => {
    const taskId = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";

    expect(isTaskMessageTaskId(taskId)).toBe(true);
    expect(taskMessagesOptions(taskId).enabled).toBe(true);
  });

  it("does not fetch task messages for optimistic task ids", () => {
    const taskId = "optimistic-optimistic-1778739487737";

    expect(isTaskMessageTaskId(taskId)).toBe(false);
    expect(taskMessagesOptions(taskId).enabled).toBe(false);
  });

  it("polls pending chat tasks even while the task id is optimistic", () => {
    const taskId = "optimistic-optimistic-1778739487737";

    expect(shouldPollPendingChatTask(taskId)).toBe(true);
  });

  it("does not poll pending chat tasks when no pending marker exists", () => {
    expect(shouldPollPendingChatTask(null)).toBe(false);
    expect(shouldPollPendingChatTask(undefined)).toBe(false);
    expect(shouldPollPendingChatTask("")).toBe(false);
  });
});
