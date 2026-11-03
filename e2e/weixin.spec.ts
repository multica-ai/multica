import { test, expect } from "@playwright/test";
import { TestApiClient } from "./fixtures";

// Real Multica login and workspace with a fixture agent and channel responses.
// Never needs a WeChat account or launches an agent CLI.
test("WeChat QR verification, connection listing, and disconnect", async ({
  page,
}, testInfo) => {
  const api = new TestApiClient();
  const run = `${Date.now()}-${process.pid}`;
  const identity = await api.login(
    `e2e-weixin-${run}@multica.ai`,
    "WeChat E2E",
  );
  const workspace = await api.ensureWorkspace("WeChat review", `weixin-${run}`);
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
        json: { ...login, status: polls > 1 ? "need_verifycode" : "scaned" },
      });
    }
    return route.fallback();
  });
  try {
    await page.goto(`/${workspace.slug}/agents/${agentID}`, {
      waitUntil: "domcontentloaded",
    });
    await page.getByRole("button", { name: "Settings", exact: true }).click();
    await page.getByRole("tab", { name: "Integrations", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Connect WeChat", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Connect WeChat", exact: true }).scrollIntoViewIfNeeded();
    await page.screenshot({
      path: testInfo.outputPath("weixin-agent.png"),
      fullPage: true,
    });
    await page
      .getByRole("button", { name: "Connect WeChat", exact: true })
      .click();
    const dialog = page.getByRole("dialog", {
      name: "Connect WeChat to WeChat Assistant",
    });
    await expect(dialog.getByRole("img", { name: "WeChat connection QR code" })).toBeVisible();
    await page.screenshot({
      path: testInfo.outputPath("weixin-qr.png"),
      fullPage: true,
    });
    await dialog.getByLabel("Verification code").fill("001234");
    await dialog.getByRole("button", { name: "Verify", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Disconnect", exact: true }),
    ).toBeVisible();
    await page.goto(
      `/${workspace.slug}/settings?tab=channels&integration=weixin`,
      { waitUntil: "domcontentloaded" },
    );
    await expect(page.getByText("Connected", { exact: true })).toBeVisible({ timeout: 30_000 });
    await page.screenshot({
      path: testInfo.outputPath("weixin-connected.png"),
      fullPage: true,
    });
    await page.getByRole("button", { name: "Disconnect", exact: true }).click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Disconnect", exact: true })
      .click();
    await expect(
      page.getByText("Connect WeChat from an agent’s Integrations tab."),
    ).toBeVisible();
  } finally {
    await api.cleanup();
  }
});
