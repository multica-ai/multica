import { expect, test } from "@playwright/test";
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { createTestApi } from "./helpers";

// Real Multica browser/API/database chain; Gongfeng v3 and webhook deliveries
// are deterministic local stubs, without an external account or agent CLI.
test("Gongfeng repository selection, historical MR, CI and merge automation", async ({ page, request }, testInfo) => {
  test.setTimeout(180_000);
  test.skip(process.env.MULTICA_VCS_INTEGRATION_ENABLED !== "true" || !process.env.MULTICA_VCS_SECRET_KEY,
    "Requires the self-hosted VCS integration in the managed test environment");
  let title = "", identifier = "", state = "opened", sha = "deadbeef", updated = "2026-10-09T08:00:00+0000";
  let hook: { id: number; url: string; token: string } | undefined;
  let validationCalls = 0, hookPosts = 0, checksPassed = false;
  let denyChecks = false, deniedChecks = 0;
  const project = { id: 55, path_with_namespace: "acme/team/widget", web_url: "https://git.code.tencent.com/acme/team/widget", ssh_url_to_repo: "git@git.code.tencent.com:acme/team/widget.git", default_branch: "main", description: "Widget" };
  const mr = () => ({ id: 901, iid: 7, title, description: `Closes ${identifier}`, state, sha, source_branch: "feature/login", source_project_id: 55, created_at: "2026-10-09T07:00:00+0000", updated_at: updated, merge_status: "can_be_merged", author: { username: "alice" } });
  const provider = createServer(async (req, res) => {
    const url = new URL(req.url!, "http://mock");
    const path = decodeURIComponent(url.pathname);
    const send = (body: unknown) => { res.writeHead(200, { "Content-Type": "application/json" }); res.end(JSON.stringify(body)); };
    if (req.headers["private-token"] !== "test-private-token") { res.writeHead(401); res.end(); return; }
    if (path === "/api/v3/user") { validationCalls++; send({ username: "alice" }); }
    else if (path === "/api/v3/projects") send([project]);
    else if (path === "/api/v3/projects/55" || path === "/api/v3/projects/acme/team/widget") send(project);
    else if (path === "/api/v3/projects/55/hooks" && req.method === "GET") send(hook ? [hook] : []);
    else if (path === "/api/v3/projects/55/hooks" && req.method === "POST") {
      let body = ""; for await (const chunk of req) body += chunk;
      const input = JSON.parse(body); expect(input.merge_requests_events).toBe(true); expect(input.enable_ssl_verification).toBe(true);
      hook = { id: 9, url: input.url, token: input.token }; hookPosts++; send(hook);
    } else if (path === "/api/v3/projects/55/hooks/9" && req.method === "GET") send(hook);
    else if (path === "/api/v3/projects/55/hooks/9" && req.method === "DELETE") { hook = undefined; res.writeHead(204); res.end(); }
    else if (path === "/api/v3/projects/55/merge_requests") send([mr()]);
    else if (path === "/api/v3/projects/55/merge_request/901") send(mr());
    else if (path === `/api/v3/projects/55/commits/${sha}/statuses`) {
      if (denyChecks) { deniedChecks++; res.writeHead(503); res.end(); return; }
      send([
        { id: 1, context: "build", state: "success" },
        { id: 2, context: "unit", state: checksPassed ? "success" : "failure" },
      ]);
    }
    else { res.writeHead(404); res.end(); }
  });
  await new Promise<void>((resolve) => provider.listen(0, "127.0.0.1", resolve));
  const address = provider.address();
  if (!address || typeof address === "string") throw new Error("Mock provider did not bind a port");
  const api = await createTestApi();
  const workspace = (await api.getWorkspaces())[0]!;
  let connectionId: string | undefined;
  try {
    const issue = await api.createIssue("Verify Gongfeng repository workflow", { status: "in_progress" });
    identifier = issue.identifier; title = `${identifier} Fix login`;
    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());
    await page.goto(`/${workspace.slug}/settings?tab=code`, { waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "Connect", exact: true }).last().click();
    const form = page.getByRole("dialog");
    await form.getByRole("combobox").click();
    await page.getByRole("option", { name: "Tencent Gongfeng" }).click();
    await expect(form.getByLabel("Instance URL")).toHaveValue("https://git.code.tencent.com");
    await page.screenshot({ path: testInfo.outputPath("gongfeng-connect.png"), fullPage: true });
    await form.getByLabel("Instance URL").fill(`http://127.0.0.1:${address.port}`);
    await form.getByLabel("Access token", { exact: true }).fill("test-private-token");
    const connected = page.waitForResponse((response) => response.request().method() === "POST" && response.url().endsWith(`/api/workspaces/${workspace.id}/vcs/connections`));
    await form.getByRole("button", { name: "Connect", exact: true }).click();
    const response = await connected, connection = await response.json(); connectionId = connection.id;
    expect(response.status()).toBe(200); expect(validationCalls).toBe(1);
    const picker = page.getByRole("dialog", { name: "Gongfeng repositories" });
    await picker.getByRole("checkbox", { name: "acme/team/widget" }).click();
    await picker.getByRole("button", { name: "Enable sync" }).click();
    // Import is driven by the repository API, before any webhook is delivered.
    await expect.poll(async () => (await api.getIssuePullRequests(issue.id)).pull_requests[0]?.checks_failed).toBe(1);
    expect(hookPosts).toBe(1); expect(hook?.token).toBe(connection.webhook_secret);
    await expect(picker.getByText("Webhook configured", { exact: true })).toBeVisible({ timeout: 20_000 });
    await page.screenshot({ path: testInfo.outputPath("gongfeng-repositories.png"), fullPage: true });
    await picker.getByRole("button", { name: "I've saved it" }).click();
    await page.getByRole("button", { name: "Add repository" }).click();
    await page.getByRole("menuitem", { name: "Choose from Gongfeng" }).click();
    const importer = page.getByRole("dialog", { name: "Gongfeng repositories" });
    await importer.getByRole("checkbox", { name: "acme/team/widget" }).click();
    await importer.getByRole("button", { name: "Add repositories" }).click();
    await expect(importer).not.toBeVisible();
    expect((await api.getWorkspaces())[0]!.repos).toContainEqual({ url: project.ssh_url_to_repo, description: project.description });
    const pullRequestsLoaded = page.waitForResponse((response) =>
      response.request().method() === "GET" &&
      response.url().endsWith(`/api/issues/${issue.id}/pull-requests`) &&
      response.ok(), { timeout: 30_000 });
    await page.goto(`/${workspace.slug}/issues/${issue.identifier}`, { waitUntil: "domcontentloaded" });
    // A cold development route may hydrate after DOMContentLoaded. Wait for
    // the sidebar's real API response before asserting the rendered MR.
    await pullRequestsLoaded;
    const card = page.getByRole("link", { name: /#7/ });
    await expect(card).toBeVisible();
    await expect(card).toHaveAttribute("href", `${project.web_url}/merge_requests/7`);
    await expect(page.getByText("1/2 failed", { exact: true })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("gongfeng-checks-failed.png"), fullPage: true });
    expect((await api.getIssue(issue.id)).status).toBe("in_progress");

    const payload = JSON.parse(await readFile("server/internal/integrations/vcs/testdata/gongfeng-merge-request.json", "utf8"));
    const attrs = payload.object_attributes; attrs.title = title; attrs.description = `Closes ${identifier}`;
    const send = async () => {
      // Stub deliveries stay local even when the server advertises a public webhook URL.
      const apiBase = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
      const delivered = await request.post(`${apiBase}/api/webhooks/vcs/${connectionId}`, { headers: { "X-Event": "Merge Request Hook", "X-Token": connection.webhook_secret }, data: payload });
      expect(delivered.status()).toBe(202);
    };
    // A failing new-head refresh publishes a realtime update. Keep the page
    // open and re-query it repeatedly: the failure must not trigger an API loop.
    denyChecks = true;
    sha = "failed-refresh-head"; updated = "2026-10-09T08:03:00+0000";
    attrs.action = "update"; attrs.last_commit.id = sha; attrs.updated_at = updated;
    await send();
    await expect.poll(async () => {
      const pr = (await api.getIssuePullRequests(issue.id)).pull_requests[0];
      return pr?.snapshot_stale && pr?.snapshot_available === false;
    }).toBe(true);
    await expect(page.getByText("1/2 failed", { exact: true })).not.toBeVisible();
    const attemptsAfterFailure = deniedChecks;
    for (let i = 0; i < 5; i++) {
      const fetched = page.waitForResponse((response) => response.request().method() === "GET" && response.url().endsWith(`/api/issues/${issue.id}/pull-requests`) && response.ok());
      await page.reload({ waitUntil: "domcontentloaded" });
      await fetched;
      await expect(card).toBeVisible();
    }
    expect(deniedChecks).toBe(attemptsAfterFailure);
    expect(attemptsAfterFailure).toBeGreaterThan(0);
    await page.screenshot({ path: testInfo.outputPath("gongfeng-refresh-unavailable.png"), fullPage: true });
    // A genuinely newer head bypasses the previous head's failure cooldown.
    denyChecks = false;
    sha = "new-head"; updated = "2026-10-09T08:05:00+0000"; checksPassed = true;
    attrs.action = "update"; attrs.last_commit.id = sha; attrs.updated_at = updated;
    await send();
    await expect.poll(async () => (await api.getIssuePullRequests(issue.id)).pull_requests[0]?.checks_rollup).toBe("success");
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByText("Checks passed", { exact: true })).toBeVisible();
    state = "merged"; updated = "2026-10-09T08:10:00+0000";
    attrs.action = "merge"; attrs.state = state; attrs.updated_at = updated; payload.user.username = "bob";
    await send();
    await expect.poll(async () => (await api.getIssue(issue.id)).status).toBe("done");
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByRole("link", { name: /#7/ })).toBeVisible();
    await expect(page.getByRole("button", { name: "Done", exact: true }).first()).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("gongfeng-merged.png"), fullPage: true });
  } finally {
    try {
      if (connectionId) await api.deleteVCSConnection(workspace.id, connectionId);
      await api.cleanup();
    } finally { await new Promise<void>((resolve, reject) => provider.close((error) => error ? reject(error) : resolve())); }
  }
});
