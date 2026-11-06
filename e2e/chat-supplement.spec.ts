import "./env";
import { expect, test } from "@playwright/test";
import pg from "pg";
import { createTestApi } from "./helpers";

// Browser wiring and durable receipt rendering. Engine transport is proved by
// TestChatSupplementEndToEnd; this test calls claim/ack without a model.
test("direct chat adds guidance explicitly and preserves a rejected draft", async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  const api = await createTestApi();
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  let runtimeId: string | undefined;
  let agentId: string | undefined;
  let sessionId: string | undefined;
  let taskId: string | undefined;
  try {
    const ws = (await api.getWorkspaces())[0]!;
    const user = await db.query(`SELECT id FROM "user" WHERE email=$1`, [api.getEmail()]);
    const userId = user.rows[0].id;
    runtimeId = (await db.query(`INSERT INTO agent_runtime (workspace_id,name,runtime_mode,provider,status,owner_id,last_seen_at,metadata) VALUES ($1,'Local Codex fixture','cloud','codex','online',$2,now(),'{}') RETURNING id`, [ws.id,userId])).rows[0].id;
    agentId = (await db.query(`INSERT INTO agent (workspace_id,name,runtime_mode,runtime_config,runtime_id,visibility,permission_mode,owner_id) VALUES ($1,'Chat steering fixture','cloud','{}',$2,'private','private',$3) RETURNING id`, [ws.id,runtimeId,userId])).rows[0].id;
    sessionId = (await db.query(`INSERT INTO chat_session (workspace_id,agent_id,creator_id,title,explicitly_created_at) VALUES ($1,$2,$3,'Active guidance test',now()) RETURNING id`, [ws.id,agentId,userId])).rows[0].id;
    taskId = (await db.query(`INSERT INTO agent_task_queue (agent_id,runtime_id,chat_session_id,status,started_at) VALUES ($1,$2,$3,'running',now()) RETURNING id`, [agentId,runtimeId,sessionId])).rows[0].id;
    await db.query(`INSERT INTO chat_message (chat_session_id,task_id,role,content) VALUES ($1,$2,'user','Investigate the failing build')`, [sessionId,taskId]);
    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(`/${ws.slug}/chat?session=${sessionId}`, { waitUntil: "domcontentloaded" });
    await expect(page.getByText("Investigate the failing build")).toBeVisible({ timeout: 45_000 });
    await expect(page.getByRole("button", { name: "Add to current run", exact: true })).toHaveCount(0);
    await page.screenshot({ path: testInfo.outputPath("before-capability.png") });
    await db.query(`INSERT INTO task_supplement_capability (task_id,workspace_id,capability) VALUES ($1,$2,'task-supplement-v1')`,[taskId,ws.id]);
    await page.reload({ waitUntil: "domcontentloaded" });
    const add = page.getByRole("button", { name: "Add to current run", exact: true });
    await expect(add).toBeVisible({ timeout: 30_000 });
    const editor = page.locator(".ProseMirror:visible").last();
    await expect(page.getByText("Investigate the failing build")).toBeVisible();
    await editor.fill("Use the second test instead. MARKER_BROWSER");
    await expect(add).toBeEnabled();
    await page.screenshot({ path: testInfo.outputPath("guidance-composer.png") });
    // The server commits, but the browser loses the response. Reloading must
    // retain the original request identity; no automatic retry is permitted.
    let originalRequestId: string | undefined;
    await page.route(`**/tasks/${taskId}/supplements`, async (route) => {
      originalRequestId = route.request().postDataJSON().client_request_id;
      const committed = await route.fetch();
      expect(committed.status()).toBe(201);
      await route.abort("failed");
    }, { times: 1 });
    await add.click();
    await expect(page.getByText("Could not confirm the addition.", { exact: false })).toBeVisible();
    await expect(editor).toHaveText("Use the second test instead. MARKER_BROWSER");
    await page.screenshot({ path: testInfo.outputPath("guidance-response-lost.png") });
    expect((await db.query(`SELECT count(*) FROM chat_supplement WHERE task_id=$1`, [taskId])).rows[0].count).toBe("1");
    await page.reload({ waitUntil: "domcontentloaded" });
    const retry = page.getByRole("button", { name: "Retry original request", exact: true });
    await expect(retry).toBeVisible({ timeout: 30_000 });
    const posted = page.waitForResponse((response) => response.request().method()==="POST" && response.url().endsWith(`/tasks/${taskId}/supplements`));
    await retry.click();
    const response = await posted;
    expect(response.status()).toBe(200);
    expect(response.request().postDataJSON().client_request_id).toBe(originalRequestId);
    const message = await response.json();
    expect(message.supplement).toMatchObject({ task_id: taskId, status: "pending" });
    await expect(editor).toBeEmpty();
    await expect(page.getByText("Waiting to send to current run")).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("guidance-pending.png") });
    const base = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
    const headers = { Authorization: `Bearer ${api.getToken()}`, "X-Workspace-ID": ws.id };
    const claim = await page.request.post(`${base}/api/daemon/tasks/${taskId}/supplements/claim`, { headers, data: {} });
    expect(claim.status()).toBe(200);
    expect((await claim.json()).comment_id).toBe(message.id);
    await expect(page.getByText("Sending to current run", { exact: true })).toBeVisible({ timeout: 30_000 });
    await expect(page.getByText("Delivery uncertain", { exact: true })).toHaveCount(0);
    const uncertain = await page.request.post(`${base}/api/daemon/tasks/${taskId}/supplements/${message.id}/ack`, { headers, data: { delivered: false, error: "delivery_unknown" } });
    expect(uncertain.status()).toBe(200);
    await expect(page.getByText("Delivery uncertain")).toBeVisible({ timeout: 30_000 });
    await page.screenshot({ path: testInfo.outputPath("guidance-uncertain-live.png") });
    const ack = await page.request.post(`${base}/api/daemon/tasks/${taskId}/supplements/${message.id}/ack`, { headers, data: { delivered: true } });
    expect(ack.status()).toBe(200);
    await expect(page.getByText("Accepted by engine")).toBeVisible({ timeout: 15_000 });
    await page.screenshot({ path: testInfo.outputPath("guidance-accepted.png") });
    // The floating window uses the second shared send controller.
    await page.goto(`/${ws.slug}/issues`, { waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "Multica is working...", exact: true }).click();
    await expect(add).toBeVisible();
    await editor.fill("Additional guidance from the floating chat");
    const floatingPosted = page.waitForResponse((response) => response.request().method()==="POST" && response.url().endsWith(`/tasks/${taskId}/supplements`));
    await add.click();
    expect((await floatingPosted).status()).toBe(201);
    await expect(page.getByText("Additional guidance from the floating chat")).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("floating-guidance.png") });
    await editor.fill("Keep this draft when the run ends");
    // The UI still holds the earlier capability: the server must recheck.
    await db.query(`UPDATE agent_task_queue SET status='completed' WHERE id=$1`,[taskId]);
    const refused = page.waitForResponse((response) => response.request().method()==="POST" && response.url().endsWith(`/tasks/${taskId}/supplements`));
    await add.click();
    expect((await refused).status()).toBe(409);
    await expect(editor).toHaveText("Keep this draft when the run ends");
    expect((await db.query(`SELECT id FROM agent_task_queue WHERE chat_session_id=$1`,[sessionId])).rows).toHaveLength(1);
    expect((await db.query(`SELECT message_id FROM chat_supplement WHERE task_id=$1`,[taskId])).rows).toHaveLength(2);
    await page.screenshot({ path: testInfo.outputPath("guidance-rejected-draft.png") });
  } finally {
    if (sessionId) await db.query(`DELETE FROM chat_supplement WHERE chat_session_id=$1`,[sessionId]);
    if (taskId) await db.query(`DELETE FROM task_supplement_capability WHERE task_id=$1`,[taskId]);
    if (sessionId) await db.query(`DELETE FROM chat_session WHERE id=$1`,[sessionId]);
    if (agentId) await db.query(`DELETE FROM agent_task_queue WHERE agent_id=$1`,[agentId]);
    if (agentId) await db.query(`DELETE FROM agent WHERE id=$1`,[agentId]);
    if (runtimeId) await db.query(`DELETE FROM agent_runtime WHERE id=$1`,[runtimeId]);
    await db.end();
    await api.cleanup();
  }
});
