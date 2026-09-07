import "./env";

import { createServer } from "node:http";
import { once } from "node:events";
import { randomUUID } from "node:crypto";
import { expect, test } from "@playwright/test";
import pg from "pg";
import { createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

// Opt in against an isolated server configured with MULTICA_LLM_BASE_URL
// pointing at this test's loopback provider (port FOLLOW_UP_LLM_PORT, default
// 55436). The browser/API/DB/WS paths are real; model and daemon boundaries
// are deterministic protocol fixtures, never ambient user-installed CLIs.
test("issue follow-ups complete the browser-to-task round trip", async ({ page }, testInfo) => {
  test.skip(process.env.MULTICA_E2E_FOLLOW_UPS !== "1", "requires isolated server and local LLM fixture");
  test.setTimeout(120_000);
  const databaseUrl = process.env.DATABASE_URL;
  if (!databaseUrl) throw new Error("Explicit isolated DATABASE_URL is required");
  const apiBase = process.env.NEXT_PUBLIC_API_URL;
  if (!apiBase || !["localhost", "127.0.0.1"].includes(new URL(apiBase).hostname)) {
    throw new Error("Follow-up E2E must target a loopback API");
  }

  const prompt = "Calculate 20 + 22 and report the verified total.";
  const actions = [
    { label: "Verify the total", prompt, primary: true },
    { label: "Review the calculation", prompt: "Review the calculation and explain the assumptions." },
  ];
  const providerRequests: string[] = [];
  const provider = createServer(async (req, res) => {
    const chunks: Buffer[] = [];
    for await (const chunk of req) chunks.push(Buffer.from(chunk));
    const body = Buffer.concat(chunks).toString();
    providerRequests.push(body);
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({
      id: "follow-up-e2e", object: "chat.completion", model: "e2e-fixture",
      choices: [{ index: 0, finish_reason: "stop", message: { role: "assistant", content: JSON.stringify({ actions }) } }],
    }));
  });
  provider.listen(Number(process.env.FOLLOW_UP_LLM_PORT ?? 55436), "127.0.0.1");
  await once(provider, "listening");

  const db = new pg.Client(databaseUrl);
  let api: TestApiClient | undefined;
  let agentId: string | undefined;
  let runtimeId: string | undefined;
  let issueId: string | undefined;
  const wsFrames: string[] = [];
  page.on("websocket", (socket) => socket.on("framereceived", ({ payload }) => {
    wsFrames.push(payload.toString());
  }));

  try {
    await db.connect();
    api = await createTestApi();
    const workspace = (await api.getWorkspaces())[0]!;
    const token = api.getToken()!;
    const request = (path: string, body: unknown = {}, method = "POST") => fetch(`${apiBase}${path}`, {
      method,
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}`, "X-Workspace-ID": workspace.id },
      body: method === "GET" ? undefined : JSON.stringify(body),
    });
    const post = async (path: string, body: unknown = {}) => {
      const response = await request(path, body);
      expect(response.ok, `${path}: ${response.status} ${await response.clone().text()}`).toBeTruthy();
      return response.json();
    };
    const registration = await post("/api/daemon/register", {
      workspace_id: workspace.id, daemon_id: randomUUID(), device_name: "Follow-up E2E fixture",
      runtimes: [{ name: "Deterministic worker", type: "codex", status: "online", version: "e2e" }],
    });
    runtimeId = registration.runtimes[0].id;
    const agent = await post("/api/agents", {
      name: "Follow-up verifier", description: "Synthetic E2E worker", runtime_id: runtimeId,
      visibility: "workspace", max_concurrent_tasks: 1, instructions: "Only process this synthetic test.",
    });
    agentId = agent.id;
    const issue = await api.createIssue("Verify contextual follow-up round trip", { status: "todo" });
    issueId = issue.id;
    const source = await post(`/api/issues/${issueId}/comments`, {
      content: `[@Follow-up verifier](mention://agent/${agentId}) Prepare a calculation for review.`,
    });
    const firstClaim = (await post(`/api/daemon/runtimes/${runtimeId}/tasks/claim`)).task;
    expect(firstClaim?.issue_id).toBe(issueId);
    await post(`/api/daemon/tasks/${firstClaim.id}/start`);

    await page.addInitScript((authToken) => {
      localStorage.setItem("multica_token", authToken);
      localStorage.setItem("multica:chat:isOpen", "false");
      localStorage.setItem("multica_locale", "en");
    }, token);
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.route("**/*", (route) => {
      const host = new URL(route.request().url()).hostname;
      return ["localhost", "127.0.0.1"].includes(host) ? route.continue() : route.abort();
    });
    // Use the canonical route so a UUID -> identifier redirect cannot unmount
    // the card between the hover and tooltip assertion.
    await page.goto(`/${workspace.slug}/issues/${issue.identifier}`, { waitUntil: "domcontentloaded" });
    await expect(page.getByText(issue.title, { exact: true }).first()).toBeVisible({ timeout: 60_000 });
    await expect.poll(() => wsFrames.length).toBeGreaterThan(0);

    // Completion creates the agent comment, then asynchronously requests and
    // persists suggestions. No suggestion rows are seeded by this test.
    await post(`/api/daemon/tasks/${firstClaim.id}/complete`, { output: "The inputs are 20 and 22. Please choose the next verification step." });
    const primary = page.getByRole("button", { name: "Verify the total", exact: true });
    await expect(primary).toBeVisible({ timeout: 20_000 });
    await expect(page.getByRole("button", { name: "Review the calculation", exact: true })).toBeVisible();
    await primary.hover();
    // The shared Base UI popup exposes data-slot, not role="tooltip".
    const tooltip = page.locator('[data-slot="tooltip-content"]');
    await expect(tooltip).toBeVisible();
    await expect(tooltip).toContainText(prompt);
    await page.screenshot({ path: testInfo.outputPath("follow-up-suggestions.png"), fullPage: true });

    const anchor = (await db.query(
      `SELECT id, parent_id, suggested_follow_ups FROM comment WHERE source_task_id = $1 AND author_type = 'agent'`, [firstClaim.id],
    )).rows[0];
    expect(anchor.parent_id).toBe(source.id);
    expect(anchor.suggested_follow_ups).toHaveLength(2);
    const actionId = anchor.suggested_follow_ups[0].id;
    expect(actionId).toMatch(/^[0-9a-f-]{36}$/);
    expect(providerRequests.some((body) => body.includes("The inputs are 20 and 22"))).toBeTruthy();
    expect(wsFrames.some((frame) => frame.includes("comment:follow_ups_updated"))).toBeTruthy();

    const activation = page.waitForResponse((response) => response.url().endsWith(`/follow-ups/${actionId}/run`));
    await primary.click();
    const response = await activation;
    expect(response.status()).toBe(201);
    const reply = await response.json();
    expect(reply.parent_id).toBe(anchor.id);
    expect(reply.content).toContain(prompt);
    expect(reply.content).toContain(`mention://agent/${agentId}`);
    expect(reply.trigger_outcomes[0].status).toBe("queued");
    await expect(primary).not.toBeVisible();
    const duplicate = await request(`/api/issues/${issueId}/comments/${anchor.id}/follow-ups/${actionId}/run`);
    expect(duplicate.status).toBe(409);

    const secondClaim = (await post(`/api/daemon/runtimes/${runtimeId}/tasks/claim`)).task;
    expect(secondClaim?.agent_id).toBe(agentId);
    expect(JSON.stringify(secondClaim)).toContain(prompt);
    await post(`/api/daemon/tasks/${secondClaim.id}/start`);
    // Deterministic worker output delivered via the same completion callback
    // a daemon uses; this is not a claim that a real Codex process ran.
    const result = `Verified total: ${20 + 22}. Both inputs were included.`;
    await post(`/api/daemon/tasks/${secondClaim.id}/complete`, { output: result });
    await expect(page.getByText(result, { exact: true }).first()).toBeVisible({ timeout: 15_000 });
    const persisted = (await db.query(
      `SELECT t.status, c.content, c.parent_id FROM agent_task_queue t
       JOIN comment c ON c.source_task_id = t.id AND c.author_type = 'agent'
       WHERE t.id = $1`, [secondClaim.id],
    )).rows;
    expect(persisted).toEqual([{ status: "completed", content: result, parent_id: reply.id }]);
    const count = await db.query(`SELECT count(*)::int AS n FROM agent_task_queue WHERE trigger_comment_id = $1`, [reply.id]);
    expect(count.rows[0].n).toBe(1);
    // Let completion events settle before capturing the finished interaction.
    await expect(page.getByText("Queued", { exact: true })).toHaveCount(0);
    await expect(primary).toBeEnabled();
    await page.mouse.move(0, 0);
    await page.screenshot({ path: testInfo.outputPath("follow-up-completed.png"), fullPage: true });
    await testInfo.attach("round-trip-evidence", { contentType: "application/json", body: JSON.stringify({
      issueId, agentId, runtimeId, sourceTaskId: firstClaim.id, anchorId: anchor.id,
      actionId, replyId: reply.id, followUpTaskId: secondClaim.id, persisted,
      providerRequests: providerRequests.length,
      receivedSuggestionEvent: wsFrames.some((frame) => frame.includes("comment:follow_ups_updated")),
    }, null, 2) });
  } catch (error) {
    await page.screenshot({ path: testInfo.outputPath("failure-before-cleanup.png"), fullPage: true });
    throw error;
  } finally {
    if (issueId) await db.query(`DELETE FROM agent_task_queue WHERE issue_id = $1`, [issueId]);
    await api?.cleanup();
    if (agentId) await db.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    if (runtimeId) await db.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await db.end();
    provider.close();
    provider.closeAllConnections();
  }
});
