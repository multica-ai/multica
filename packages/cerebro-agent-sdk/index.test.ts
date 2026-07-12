import { describe, expect, it, vi } from "vitest";

import { MulticaAgentClient, parseUIMessageStream } from "./index";

describe("MulticaAgentClient", () => {
  it("scopes every request to one workspace and agent", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(new Response(JSON.stringify({ id: "session-1" }), { status: 201 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ message_id: "message-1", task_id: "task-1" }), { status: 201 }));
    const client = new MulticaAgentClient({
      baseUrl: "https://multica.example/",
      token: "secret",
      workspaceId: "workspace-1",
      agentId: "rune-1",
      fetcher,
    });

    const session = await client.createSession("Finance");
    await client.sendMessage(session.id, "June P&L");

    expect(fetcher).toHaveBeenNthCalledWith(
      1,
      "https://multica.example/api/chat/sessions",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({
          Authorization: "Bearer secret",
          "X-Workspace-Id": "workspace-1",
        }),
        body: JSON.stringify({ agent_id: "rune-1", title: "Finance" }),
      }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      "https://multica.example/api/chat/sessions/session-1/messages",
      expect.objectContaining({ body: JSON.stringify({ content: "June P&L" }) }),
    );
  });

  it("opens a resumable UI-message stream for the exact task", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response('data: {"type":"text-delta","id":"text-0","delta":"hello"}\n\ndata: [DONE]\n\n', {
        status: 200,
        headers: { "content-type": "text/event-stream", "x-vercel-ai-ui-message-stream": "v1" },
      }),
    );
    const client = new MulticaAgentClient({
      baseUrl: "https://multica.example",
      token: "secret",
      workspaceId: "workspace-1",
      agentId: "rune-1",
      fetcher,
    });

    const response = await client.streamRun("session-1", "task-1");
    const chunks = [];
    for await (const chunk of parseUIMessageStream(response)) chunks.push(chunk);

    expect(chunks).toEqual([{ type: "text-delta", id: "text-0", delta: "hello" }]);
    expect(fetcher).toHaveBeenCalledWith(
      "https://multica.example/api/chat/sessions/session-1/stream?task_id=task-1",
      expect.objectContaining({ method: "GET" }),
    );
  });

  it("returns null when there is no active stream to resume", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(null, { status: 204 }));
    const client = new MulticaAgentClient({
      baseUrl: "https://multica.example",
      token: "secret",
      workspaceId: "workspace-1",
      agentId: "rune-1",
      fetcher,
    });
    await expect(client.resumeRun("session-1")).resolves.toBeNull();
  });
});
