/**
 * E2E: chat attachment upload + send back-fills the message link.
 *
 * Stays at the HTTP layer (auth → upload-file → send-chat-message → DB
 * check) so the test doesn't depend on a real agent runtime being online.
 * The UI wiring is covered by `chat-input.test.tsx` in @multica/views; this
 * spec is the end-to-end contract proof: the backend really does persist
 * chat_session_id at upload and back-fill chat_message_id at send.
 */
import "./env";
import { test, expect } from "@playwright/test";
import pg from "pg";
import { createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

const API_BASE =
  process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
const DATABASE_URL =
  process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

interface UploadRow {
  id: string;
  url: string;
  chat_session_id: string | null;
  chat_message_id: string | null;
}

async function authedFetch(api: TestApiClient, path: string, init?: RequestInit) {
  const token = api.getToken();
  if (!token) throw new Error("test api client not logged in");
  const headers: Record<string, string> = {
    Authorization: `Bearer ${token}`,
    ...((init?.headers as Record<string, string>) ?? {}),
  };
  return fetch(`${API_BASE}${path}`, { ...init, headers });
}

test.describe("Chat attachments", () => {
  let api: TestApiClient;
  let pgClient: pg.Client | null = null;
  let createdSessionId: string | null = null;
  let createdAgentId: string | null = null;
  let createdRuntimeId: string | null = null;

  test.beforeEach(async () => {
    api = await createTestApi();
    pgClient = new pg.Client(DATABASE_URL);
    await pgClient.connect();
  });

  test.afterEach(async () => {
    try {
      if (pgClient) {
        if (createdSessionId) {
          await pgClient.query(`DELETE FROM chat_session WHERE id = $1`, [createdSessionId]);
        }
        if (createdAgentId) {
          await pgClient.query(`DELETE FROM agent WHERE id = $1`, [createdAgentId]);
        }
        if (createdRuntimeId) {
          await pgClient.query(`DELETE FROM agent_runtime WHERE id = $1`, [createdRuntimeId]);
        }
      }
    } finally {
      if (pgClient) await pgClient.end();
      pgClient = null;
      createdSessionId = null;
      createdAgentId = null;
      createdRuntimeId = null;
      await api.cleanup();
    }
  });

  test("upload-file binds attachment to the chat_session; send back-fills chat_message_id", async () => {
    expect(pgClient).not.toBeNull();
    const pgc = pgClient!;

    // Resolve the workspace + caller so we can seed an agent/runtime/session
    // directly via SQL. Going through the HTTP API would require modelling
    // local-daemon ownership which isn't needed for this contract test.
    const workspaces = await api.getWorkspaces();
    const ws = workspaces[0]!;
    api.setWorkspaceSlug(ws.slug);
    api.setWorkspaceId(ws.id);

    const userRow = await pgc.query(
      `SELECT id FROM "user" WHERE email = $1 LIMIT 1`,
      ["e2e@multica.ai"],
    );
    if (userRow.rows.length === 0) throw new Error("e2e user missing");
    const userId = userRow.rows[0].id as string;

    // Seed runtime + agent + chat_session.
    const runtimeIns = await pgc.query(
      `INSERT INTO agent_runtime (
         workspace_id, daemon_id, name, runtime_mode, provider, status,
         device_info, metadata, last_seen_at
       )
       VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, now())
       RETURNING id`,
      [ws.id, `e2e chat runtime ${Date.now()}`, "e2e_chat_runtime", "E2E chat runtime"],
    );
    createdRuntimeId = runtimeIns.rows[0].id as string;

    const agentIns = await pgc.query(
      `INSERT INTO agent (
         workspace_id, name, description, runtime_mode, runtime_config,
         runtime_id, visibility, max_concurrent_tasks, owner_id
       )
       VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
       RETURNING id`,
      [ws.id, `E2E Chat Agent ${Date.now()}`, createdRuntimeId, userId],
    );
    createdAgentId = agentIns.rows[0].id as string;

    const sessionIns = await pgc.query(
      `INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
       VALUES ($1, $2, $3, 'E2E Chat Attachment Session', 'active')
       RETURNING id`,
      [ws.id, createdAgentId, userId],
    );
    createdSessionId = sessionIns.rows[0].id as string;

    // 1. Upload a small PNG against the chat session.
    const pngBytes = Buffer.from([
      0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
      0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR
    ]);
    const form = new FormData();
    form.append("file", new Blob([new Uint8Array(pngBytes)], { type: "image/png" }), "e2e.png");
    form.append("chat_session_id", createdSessionId);
    const uploadRes = await authedFetch(api, "/api/upload-file", {
      method: "POST",
      body: form,
      headers: { "X-Workspace-Slug": ws.slug },
    });
    expect(uploadRes.status).toBe(200);
    const uploaded = (await uploadRes.json()) as UploadRow;
    expect(uploaded.chat_session_id).toBe(createdSessionId);
    expect(uploaded.chat_message_id).toBeNull();
    expect(uploaded.url).toBeTruthy();

    // 2. Send a chat message that references the attachment.
    const sendRes = await authedFetch(api, `/api/chat/sessions/${createdSessionId}/messages`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Workspace-Slug": ws.slug,
      },
      body: JSON.stringify({
        content: `look at this ![](${uploaded.url})`,
        attachment_ids: [uploaded.id],
      }),
    });
    expect(sendRes.status).toBe(201);
    const sendBody = (await sendRes.json()) as { message_id: string; task_id: string };
    expect(sendBody.message_id).toBeTruthy();

    // 3. DB check: the attachment row's chat_message_id matches the new message.
    const after = await pgc.query<{ chat_message_id: string | null }>(
      `SELECT chat_message_id::text FROM attachment WHERE id = $1`,
      [uploaded.id],
    );
    expect(after.rows[0]?.chat_message_id).toBe(sendBody.message_id);

    // 4. Clean up the attachment we created (chat_session cascade handles the
    //    rest in afterEach via chat_session row deletion).
    await pgc.query(`DELETE FROM attachment WHERE id = $1`, [uploaded.id]);
  });
});
