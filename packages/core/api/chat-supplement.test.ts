// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { ChatMessageSchema, ChatPendingTaskSchema } from "./schemas";

afterEach(() => vi.unstubAllGlobals());

describe("Chat supplement API compatibility", () => {
  it.each(["pending", "delivering", "delivered", "failed", "unknown"])("preserves the %s receipt state", (status) => {
    expect(ChatMessageSchema.parse({ id: "m", chat_session_id: "s", supplement: { task_id: "t", status } }).supplement?.status).toBe(status);
  });
  it("fails closed for absent or malformed capabilities", () => {
    expect(ChatPendingTaskSchema.parse({}).can_supplement).toBeUndefined();
    expect(ChatPendingTaskSchema.parse({ can_supplement: "true" }).can_supplement).toBe(false);
  });
  it("preserves legacy messages and degrades unknown receipts honestly", () => {
    const message = { id: "m", chat_session_id: "s" };
    expect(ChatMessageSchema.parse(message).supplement).toBeUndefined();
    expect(ChatMessageSchema.parse({ ...message, supplement: { task_id: "t", status: "future" } }).supplement?.status).toBe("unknown");
    expect(ChatMessageSchema.parse({ ...message, supplement: 42 }).supplement).toBeUndefined();
  });
  it("sends an exact target and idempotency key and rejects invalid responses", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ id: "m", chat_session_id: "s", supplement: { task_id: "t", status: "pending" } }), { status: 201 })).mockResolvedValueOnce(new Response("{}", { status: 200 }));
    vi.stubGlobal("fetch", fetch);
    const client = new ApiClient("https://api.example.test");
    await client.addToChatRun("s", "t", "guidance", "request");
    expect(fetch.mock.calls[0]?.[0]).toBe("https://api.example.test/api/chat/sessions/s/tasks/t/supplements");
    expect(JSON.parse(fetch.mock.calls[0]?.[1].body)).toEqual({ content: "guidance", client_request_id: "request" });
    await expect(client.addToChatRun("s", "t", "guidance", "request")).rejects.toThrow("invalid chat supplement response");
  });
});
