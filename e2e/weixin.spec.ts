import { test, expect } from "@playwright/test";
import { TestApiClient } from "./fixtures";

// Real Multica login and workspace with a fixture agent and channel responses.
// Never needs a WeChat account or launches an agent CLI.
test("An agent owner can connect and disconnect WeChat as a workspace member", async ({
  page,
}, testInfo) => {
  const admin = new TestApiClient();
  const api = new TestApiClient();
  const run = `${Date.now()}-${process.pid}`;
  await admin.login(`e2e-weixin-admin-${run}@example.test`, "Workspace Owner");
  const workspace = await admin.ensureWorkspace("WeChat review", `weixin-${run}`);
  const identity = await api.login(
    `e2e-weixin-member-${run}@example.test`,
    "Agent Owner",
  );
  await admin.addMemberByEmail(api.getEmail());
  api.setWorkspaceId(workspace.id);
  api.setWorkspaceSlug(workspace.slug);
  await api.markUserOnboarded();
  const token = api.getToken();
  if (!token) throw new Error("test login returned no token");
  await page.addInitScript((value) => {
    localStorage.setItem("multica_token", value);
    localStorage.setItem("multica:chat:isOpen", "false");
  }, token);
  const agentID = "11111111-1111-4111-8111-111111111111";
  const agent = {
    id: agentID,
    workspace_id: workspace.id,
    owner_id: identity.user.id,
    name: "WeChat Assistant",
    description: "",
    instructions: "",
    status: "idle",
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    max_concurrent_tasks: 1,
    model: "",
    skills: [],
    archived_at: null,
    created_at: "2026-11-03T00:00:00Z",
    updated_at: "2026-11-03T00:00:00Z",
  };
  let connected = false;
  let polls = 0;
  const login = {
    id: "22222222-2222-4222-8222-222222222222",
    status: "wait",
    qr_code_url: "https://weixin.qq.com/multica-test-fixture-not-a-live-login",
    expires_at: new Date(Date.now() + 300_000).toISOString(),
  };
  await page.route("**/api/agents**", (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/api/agents"))
      return route.fulfill({ json: [agent] });
    if (url.pathname.endsWith(`/api/agents/${agentID}`))
      return route.fulfill({ json: agent });
    return route.fallback();
  });
  await page.route("**/weixin/**", (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (request.method() === "DELETE") {
      connected = false;
      return route.fulfill({ status: 204 });
    }
    if (url.pathname.endsWith("/installations"))
      return route.fulfill({
        json: {
          configured: true,
          install_supported: true,
          installations: connected
            ? [
                {
                  id: "bot-1",
                  workspace_id: workspace.id,
                  agent_id: agentID,
                  status: "active",
                  bot_id: "test-bot",
                  user_id: "test-scanner",
                  installed_at: new Date().toISOString(),
                },
              ]
            : [],
        },
      });
    if (url.pathname.endsWith("/logins")) return route.fulfill({ json: login });
    if (url.pathname.endsWith("/poll")) {
      const body = request.postDataJSON();
      if (body.verify_code) {
        expect(body.verify_code).toBe("001234");
        connected = true;
        return route.fulfill({
          json: {
            ...login,
            status: "confirmed",
            installation_id: "bot-1",
            qr_code_url: undefined,
          },
        });
      }
      polls++;
      return route.fulfill({
        json: { ...login, status: polls > 1 ? "confirmed_but_need_verifycode" : "scaned" },
      });
    }
    return route.fallback();
  });
  try {
    await page.goto(`/${workspace.slug}/agents/${agentID}`, {
      waitUntil: "domcontentloaded",
    });
    const channels = page.locator("#messaging-channels");
    await channels.getByRole("button", { name: "Add messaging channel" }).first().click();
    const connectDialog = page.getByRole("dialog", { name: "Connect messaging channel" });
    const wechat = connectDialog.getByRole("button", { name: "WeChat", exact: true });
    await expect(wechat).toBeEnabled();
    await connectDialog.screenshot({ path: testInfo.outputPath("weixin-member-platform.png") });
    await wechat.click();
    // Choosing WeChat shows the QR straight away, with the guide below it.
    await expect(
      connectDialog.getByRole("img", { name: "WeChat connection QR code" }),
    ).toBeVisible();
    await expect(
      connectDialog.getByRole("button", { name: "Setup guide" }),
    ).toBeVisible();
    await connectDialog.screenshot({ path: testInfo.outputPath("weixin-qr.png") });
    await expect(connectDialog.getByLabel("Verification code")).toBeVisible();
    await page.screenshot({
      path: testInfo.outputPath("weixin-verification.png"),
      fullPage: true,
    });
    await connectDialog.getByLabel("Verification code").fill("001234");
    await connectDialog
      .getByRole("button", { name: "Verify", exact: true })
      .click();
    // A confirmed login closes the dialog and the agent lists the channel.
    await expect(connectDialog).toBeHidden();
    await expect(page.getByRole("button", { name: "Manage WeChat" })).toBeVisible();
    await page.screenshot({
      path: testInfo.outputPath("weixin-agent-connected.png"),
      fullPage: true,
    });
    await page.goto(
      `/${workspace.slug}/settings?tab=channels&integration=weixin`,
      { waitUntil: "domcontentloaded" },
    );
    await expect(page.getByRole("link", { name: "WeChat Assistant" })).toBeVisible({ timeout: 30_000 });
    await page.screenshot({
      path: testInfo.outputPath("weixin-connected.png"),
      fullPage: true,
    });
    await page.getByRole("link", { name: "WeChat Assistant" }).click();
    await page.getByRole("button", { name: "Manage WeChat" }).click();
    await page.getByRole("menuitem", { name: "Disconnect" }).click();
    await page.getByRole("alertdialog").getByRole("button", { name: "Disconnect", exact: true }).click();
    await expect(page.getByRole("button", { name: "Manage WeChat" })).toHaveCount(0);
  } finally {
    await api.cleanup();
    await admin.deleteWorkspace();
  }
});
