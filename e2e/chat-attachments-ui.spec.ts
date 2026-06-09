/**
 * E2E (UI): chat-message attachments render on the bubble with open + download.
 *
 * TECH-3183 — the backend already stores + serves files attached to a chat
 * message; this proves the chat window now SHOWS them. Seeds a session with one
 * user message (file attached via the real upload + send API) and one assistant
 * message (file attached the way an agent does mid-turn), opens the chat, and
 * asserts the attachment card + "Open in viewer" + "Download" controls render.
 * Captures a screenshot as the visual proof for the issue.
 */
import "./env";
import { test, expect } from "@playwright/test";
import pg from "pg";
import { loginAsDefault } from "./helpers";
import { TestApiClient } from "./fixtures";

const API_BASE =
  process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
const DATABASE_URL =
  process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

async function uploadMarkdown(token: string, slug: string, sessionId: string, name: string) {
  const form = new FormData();
  form.append(
    "file",
    new Blob([`# ${name}\n\nDette er en testrapport.`], { type: "text/markdown" }),
    name,
  );
  form.append("chat_session_id", sessionId);
  const res = await fetch(`${API_BASE}/api/upload-file`, {
    method: "POST",
    headers: { Authorization: `Bearer ${token}`, "X-Workspace-Slug": slug },
    body: form,
  });
  expect(res.status).toBe(200);
  return (await res.json()) as { id: string; url: string; download_url: string };
}

test("chat message attachments render with open + download", async ({ page }) => {
  const slug = await loginAsDefault(page);

  const api = new TestApiClient();
  await api.login("e2e@multica.ai", "E2E User");
  const ws = (await api.getWorkspaces())[0]!;
  const token = api.getToken()!;

  const pgc = new pg.Client(DATABASE_URL);
  await pgc.connect();

  const userId = (
    await pgc.query(`SELECT id FROM "user" WHERE email = $1 LIMIT 1`, ["e2e@multica.ai"])
  ).rows[0].id as string;

  const runtimeId = (
    await pgc.query(
      `INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
       VALUES ($1, NULL, $2, 'cloud', 'e2e_chat_runtime', 'online', 'E2E', '{}'::jsonb, now()) RETURNING id`,
      [ws.id, `ui-att-runtime ${Date.now()}`],
    )
  ).rows[0].id as string;

  const agentId = (
    await pgc.query(
      `INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
       VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4) RETURNING id`,
      [ws.id, `UI Att Agent ${Date.now()}`, runtimeId, userId],
    )
  ).rows[0].id as string;

  const sessionId = (
    await pgc.query(
      `INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
       VALUES ($1, $2, $3, 'TECH-3183 attachment demo', 'active') RETURNING id`,
      [ws.id, agentId, userId],
    )
  ).rows[0].id as string;

  try {
    // 1. User message with an attached file via the real upload + send API.
    const userFile = await uploadMarkdown(token, slug, sessionId, "rapport.md");
    const sendRes = await fetch(`${API_BASE}/api/chat/sessions/${sessionId}/messages`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
        "X-Workspace-Slug": slug,
      },
      body: JSON.stringify({ content: "Her er rapporten 👇", attachment_ids: [userFile.id] }),
    });
    expect(sendRes.status).toBe(201);

    // 2. Assistant message with a file attached the way an agent does mid-turn
    //    (upload to the session, then link by chat_message_id).
    const asstMsgId = (
      await pgc.query(
        `INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'assistant', $2) RETURNING id`,
        [sessionId, "Selvfølgelig — her er det færdige dokument:"],
      )
    ).rows[0].id as string;
    const asstFile = await uploadMarkdown(token, slug, sessionId, "resultat.md");
    await pgc.query(`UPDATE attachment SET chat_message_id = $1 WHERE id = $2`, [
      asstMsgId,
      asstFile.id,
    ]);

    // 3. Open the chat and verify both bubbles show their attachment + controls.
    await page.goto(`/${slug}/inbox?chat=${sessionId}`, { waitUntil: "domcontentloaded" });

    await expect(page.getByText("Her er rapporten")).toBeVisible({ timeout: 20000 });
    await expect(page.getByText("rapport.md")).toBeVisible({ timeout: 20000 });
    await expect(page.getByText("resultat.md")).toBeVisible({ timeout: 20000 });
    await expect(page.getByRole("button", { name: "Download" }).first()).toBeVisible();
    await expect(page.getByRole("button", { name: "Open in viewer" }).first()).toBeVisible();

    await page.screenshot({ path: "e2e/.artifacts/tech-3183-chat-attachments.png", fullPage: true });
  } finally {
    await pgc.query(`DELETE FROM chat_session WHERE id = $1`, [sessionId]);
    await pgc.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    await pgc.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await pgc.end();
    await api.cleanup();
  }
});
