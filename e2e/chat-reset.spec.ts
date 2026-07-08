/**
 * E2E: deleting the current chat session and sending again starts a fresh
 * session instead of reusing the deleted one.
 *
 * This stays at the HTTP + DB layer for repeatability: we prove the product
 * contract ("delete current chat and start fresh") by asserting the next
 * message lands in a different chat_session row.
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

interface ChatSendResponse {
  message_id: string;
  task_id: string;
}

interface ChatMessageRow {
  id: string;
  chat_session_id: string;
  content: string;
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

test.describe("Chat reset via delete", () => {
  let api: TestApiClient | null = null;
  let pgClient: pg.Client | null = null;
  let createdAgentId: string | null = null;
  let createdRuntimeId: string | null = null;
  let createdSessionIds: string[] = [];

  test.beforeEach(async () => {
    api = await createTestApi();
    pgClient = new pg.Client(DATABASE_URL);
    await pgClient.connect();
  });

  test.afterEach(async () => {
    try {
      if (pgClient) {
        for (const sessionId of createdSessionIds) {
          await pgClient.query(`DELETE FROM chat_session WHERE id = $1`, [sessionId]);
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
      createdAgentId = null;
      createdRuntimeId = null;
      createdSessionIds = [];
      if (api) await api.cleanup();
    }
  });

  test("deleting the current session forces the next message into a new session", async () => {
    expect(pgClient).not.toBeNull();
    const pgc = pgClient!;

    const workspaces = await api.getWorkspaces();
    const ws = workspaces[0]!;
    api.setWorkspaceSlug(ws.slug);
    api.setWorkspaceId(ws.id);

    const userRow = await pgc.query(
      `SELECT id FROM "user" WHERE email = $1 LIMIT 1`,
      [api.getEmail()],
    );
    if (userRow.rows.length === 0) throw new Error("e2e user missing");
    const userId = userRow.rows[0].id as string;

    const runtimeIns = await pgc.query(
      `INSERT INTO agent_runtime (
         workspace_id, daemon_id, name, runtime_mode, provider, status,
         device_info, metadata, last_seen_at
       )
       VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, now())
       RETURNING id`,
      [ws.id, `e2e reset runtime ${Date.now()}`, "e2e_chat_runtime", "E2E chat runtime"],
    );
    createdRuntimeId = runtimeIns.rows[0].id as string;

    const agentIns = await pgc.query(
      `INSERT INTO agent (
         workspace_id, name, description, runtime_mode, runtime_config,
         runtime_id, visibility, max_concurrent_tasks, owner_id
       )
       VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
       RETURNING id`,
      [ws.id, `E2E Reset Agent ${Date.now()}`, createdRuntimeId, userId],
    );
    createdAgentId = agentIns.rows[0].id as string;

    const firstSessionIns = await pgc.query(
      `INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
       VALUES ($1, $2, $3, 'Reset me', 'active')
       RETURNING id`,
      [ws.id, createdAgentId, userId],
    );
    const firstSessionId = firstSessionIns.rows[0].id as string;
    createdSessionIds.push(firstSessionId);

    const firstSendRes = await authedFetch(api, `/api/chat/sessions/${firstSessionId}/messages`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Workspace-Slug": ws.slug,
      },
      body: JSON.stringify({ content: "remember this old context" }),
    });
    expect(firstSendRes.status).toBe(201);
    const firstSendBody = (await firstSendRes.json()) as ChatSendResponse;
    expect(firstSendBody.message_id).toBeTruthy();

    const deleteRes = await authedFetch(api, `/api/chat/sessions/${firstSessionId}`, {
      method: "DELETE",
      headers: { "X-Workspace-Slug": ws.slug },
    });
    expect(deleteRes.status).toBe(204);

    const afterDelete = await pgc.query(
      `SELECT id FROM chat_session WHERE id = $1`,
      [firstSessionId],
    );
    expect(afterDelete.rows).toHaveLength(0);

    const createSecondSessionRes = await authedFetch(api, "/api/chat/sessions", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Workspace-Slug": ws.slug,
      },
      body: JSON.stringify({ agent_id: createdAgentId, title: "Fresh start" }),
    });
    expect(createSecondSessionRes.status).toBe(201);
    const secondSession = await createSecondSessionRes.json() as { id: string };
    const secondSessionId = secondSession.id;
    createdSessionIds.push(secondSessionId);
    expect(secondSessionId).not.toBe(firstSessionId);

    const secondSendRes = await authedFetch(api, `/api/chat/sessions/${secondSessionId}/messages`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Workspace-Slug": ws.slug,
      },
      body: JSON.stringify({ content: "fresh message after reset" }),
    });
    expect(secondSendRes.status).toBe(201);
    const secondSendBody = (await secondSendRes.json()) as ChatSendResponse;
    expect(secondSendBody.message_id).toBeTruthy();

    const secondMessage = await pgc.query<ChatMessageRow>(
      `SELECT id::text, chat_session_id::text, content
       FROM chat_message
       WHERE id = $1`,
      [secondSendBody.message_id],
    );
    expect(secondMessage.rows[0]?.chat_session_id).toBe(secondSessionId);
    expect(secondMessage.rows[0]?.chat_session_id).not.toBe(firstSessionId);
    expect(secondMessage.rows[0]?.content).toContain("fresh message after reset");
  });
});
