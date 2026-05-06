import { describe, expect, it, vi } from "vitest";
import { chatKeys } from "../chat/queries";
import { invalidateChatQueriesOnReconnect } from "./chat-reconnect";

describe("invalidateChatQueriesOnReconnect", () => {
  it("invalidates workspace chat and sessionless chat caches", () => {
    const qc = { invalidateQueries: vi.fn() };

    invalidateChatQueriesOnReconnect(qc, "ws-1");

    expect(qc.invalidateQueries).toHaveBeenCalledWith({ queryKey: chatKeys.all("ws-1") });
    expect(qc.invalidateQueries).toHaveBeenCalledWith({ queryKey: ["chat", "messages"] });
    expect(qc.invalidateQueries).toHaveBeenCalledWith({ queryKey: ["chat", "pending-task"] });
    expect(qc.invalidateQueries).toHaveBeenCalledWith({ queryKey: ["task-messages"] });
  });

  it("still invalidates sessionless caches without a current workspace", () => {
    const qc = { invalidateQueries: vi.fn() };

    invalidateChatQueriesOnReconnect(qc, null);

    expect(qc.invalidateQueries).not.toHaveBeenCalledWith({ queryKey: chatKeys.all("ws-1") });
    expect(qc.invalidateQueries).toHaveBeenCalledWith({ queryKey: ["chat", "messages"] });
    expect(qc.invalidateQueries).toHaveBeenCalledWith({ queryKey: ["chat", "pending-task"] });
    expect(qc.invalidateQueries).toHaveBeenCalledWith({ queryKey: ["task-messages"] });
  });
});
