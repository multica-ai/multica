import { describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";

vi.mock("@/data/api", () => ({ api: {} }));

import {
  refetchActiveChat,
  shouldPollActiveChat,
} from "./chat-polling";
import { chatKeys } from "./chat";

const SESSION = "session-1";
const TASK = "11111111-1111-4111-8111-111111111111";

describe("shouldPollActiveChat", () => {
  it.each([
    [true, true, SESSION, TASK, true],
    [false, true, SESSION, TASK, false],
    [true, false, SESSION, TASK, false],
    [true, true, null, TASK, false],
    [true, true, SESSION, null, false],
  ])("gates polling to a foreground focused session with a pending task", (tabFocused, appActive, sessionId, taskId, expected) => {
    expect(shouldPollActiveChat(tabFocused, appActive, sessionId, taskId)).toBe(expected);
  });
});

describe("refetchActiveChat", () => {
  it("refetches messages, pending state, and the server task timeline", async () => {
    const qc = new QueryClient();
    const refetch = vi.spyOn(qc, "refetchQueries").mockResolvedValue();

    await refetchActiveChat(qc, SESSION, TASK);

    expect(refetch).toHaveBeenCalledWith({ queryKey: chatKeys.messages(SESSION) });
    expect(refetch).toHaveBeenCalledWith({ queryKey: chatKeys.pendingTask(SESSION) });
    expect(refetch).toHaveBeenCalledWith({ queryKey: chatKeys.taskMessages(TASK) });
  });

  it("never requests task messages for an optimistic task id", async () => {
    const qc = new QueryClient();
    const refetch = vi.spyOn(qc, "refetchQueries").mockResolvedValue();

    await refetchActiveChat(qc, SESSION, "optimistic-123");

    expect(refetch).toHaveBeenCalledTimes(2);
    expect(refetch).not.toHaveBeenCalledWith({
      queryKey: chatKeys.taskMessages("optimistic-123"),
    });
  });
});
