import { test, expect } from "@playwright/test";
import pg from "pg";
import { createTestApi } from "./helpers";

test("issue loads compact runs until Runs requests full own or subtree history", async ({ page }) => {
  const api = await createTestApi();
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  let runtimeId = "";
  let agentId = "";
  try {
    const workspace = (await api.getWorkspaces())[0]!;
    const userId = (await db.query('SELECT id FROM "user" WHERE email=$1', [api.getEmail()])).rows[0].id;
    const issue = await api.createIssue("On-demand run history");
    const child = await api.createIssue("Child run history", { parent_issue_id: issue.id });

    // Seed finished runs without dispatching any work to a real runtime.
    runtimeId = (await db.query(`
      INSERT INTO agent_runtime (workspace_id,name,runtime_mode,provider,status,device_info,metadata,owner_id)
      VALUES ($1,'History test runtime','cloud','test','offline','Test','{}',$2) RETURNING id
    `, [workspace.id, userId])).rows[0].id;
    agentId = (await db.query(`
      INSERT INTO agent (workspace_id,name,description,instructions,runtime_mode,runtime_config,runtime_id,visibility,permission_mode,max_concurrent_tasks,owner_id)
      VALUES ($1,'History test agent','','','cloud','{}',$2,'private','private',1,$3) RETURNING id
    `, [workspace.id, runtimeId, userId])).rows[0].id;
    for (const issueId of [issue.id, child.id]) {
      await db.query(`
        INSERT INTO agent_task_queue (agent_id,runtime_id,issue_id,status,result,handoff_note,started_at,completed_at)
        VALUES ($1,$2,$3,'completed',$4,'Full handoff details',now()-interval '1 minute',now())
      `, [agentId, runtimeId, issueId, JSON.stringify({
        comment: "Inline run summary",
        output: `Full provider output for ${issueId}`,
        session_id: "history-test-session",
      })]);
    }

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());
    const isIssueTasks = (url: URL) => url.pathname.endsWith(`/issues/${issue.id}/task-runs`);
    const requests: URL[] = [];
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (isIssueTasks(url)) requests.push(url);
    });
    const compactResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return isIssueTasks(url) && url.searchParams.get("view") === "timeline";
    });
    await page.goto(`/${workspace.slug}/issues/${issue.id}`);
    const compact = await (await compactResponse).json();
    expect(compact).toHaveLength(1);
    expect(compact[0].result).toEqual({ comment: "Inline run summary" });
    expect(compact[0].handoff_note).toBeFalsy();
    const openRuns = page.getByRole("button", { name: /^Open timeline/ });
    await expect(openRuns).toBeVisible();
    expect(requests.length).toBeGreaterThan(0);
    expect(requests.every((url) => url.searchParams.get("view") === "timeline")).toBe(true);

    const ownHistory = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return isIssueTasks(url) && !url.searchParams.has("view") && !url.searchParams.has("scope");
    });
    await openRuns.click();
    const own = await (await ownHistory).json();
    expect(own).toHaveLength(1);
    expect(own[0].result.output).toBe(`Full provider output for ${issue.id}`);
    expect(own[0].handoff_note).toBe("Full handoff details");

    const subtreeHistory = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return isIssueTasks(url) && !url.searchParams.has("view") && url.searchParams.get("scope") === "subtree";
    });
    const includeChildren = page.getByRole("dialog").getByRole("checkbox", { name: "Include sub-issues" });
    await includeChildren.check();
    const subtree = await (await subtreeHistory).json();
    expect(subtree).toHaveLength(2);
    expect(subtree.map((task: { result: { output: string } }) => task.result.output).sort()).toEqual(
      [issue.id, child.id].map((id) => `Full provider output for ${id}`).sort(),
    );
    expect(subtree.every((task: { handoff_note: string }) => task.handoff_note === "Full handoff details")).toBe(true);
    await expect(includeChildren).toBeChecked();
  } finally {
    await api.cleanup();
    if (agentId) await db.query("DELETE FROM agent_task_queue WHERE agent_id=$1", [agentId]);
    if (agentId) await db.query("DELETE FROM agent WHERE id=$1", [agentId]);
    if (runtimeId) await db.query("DELETE FROM agent_runtime WHERE id=$1", [runtimeId]);
    await db.end();
  }
});
