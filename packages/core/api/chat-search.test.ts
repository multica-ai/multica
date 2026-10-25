import { describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

describe("ApiClient chat search", () => {
  it("parses chat message search results and tolerates a malformed list", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            messages: [
              {
                id: "m1",
                chat_session_id: "s1",
                role: "user",
                content: "deploy the api",
                created_at: "2026-01-01T00:00:00.5Z",
                match_source: "content",
                matched_snippet: "deploy the api",
                session: { id: "s1", agent_id: "a1", title: "Deploy chat", status: "active" },
              },
            ],
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ messages: "invalid" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    const ok = await client.searchChatMessages({ q: "deploy" });
    expect(ok.messages[0]).toMatchObject({ id: "m1", session: { title: "Deploy chat" } });
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain(
      "/api/chat/sessions/search?q=deploy",
    );

    const degraded = await client.searchChatMessages({ q: "deploy" });
    expect(degraded.messages).toEqual([]);
  });

  it("sends the chat message cursor on snapshot pages only once there is one", async () => {
    const fetchMock = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            issues: [],
            comments: [],
            projects: [],
            sessions: [],
            messages: [],
            next_after_number: 0,
            next_after_message_id: "",
            done: true,
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    await client.getSearchIndexSnapshot({ workspaceSlug: "ws", afterNumber: 0 });
    expect(String(fetchMock.mock.calls[0]?.[0])).not.toContain("after_message_id");

    await client.getSearchIndexSnapshot({ workspaceSlug: "ws", afterNumber: 3, afterMessageId: "m-1" });
    expect(String(fetchMock.mock.calls[1]?.[0])).toContain("after_message_id=m-1");
  });
});
