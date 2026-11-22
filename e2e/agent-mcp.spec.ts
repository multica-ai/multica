import { test, expect, type Page } from "@playwright/test";
import { TestApiClient } from "./fixtures";
import { waitForPageText } from "./helpers";

// Agent Apps section and Advanced Settings Apps management (MUL-8990):
//
// Auth + workspace bootstrap go through the real backend (same as every other
// spec), while the agent list and connected-apps endpoints are mocked at the
// network boundary so the test runs without a configured COMPOSIO_API_KEY or a
// live runtime. The PUT /api/agents/<id>/apps write is intercepted so we can
// assert the exact payload the toggle produces.

const E2E_WORKER =
  process.env.TEST_PARALLEL_INDEX ?? process.env.TEST_WORKER_INDEX ?? "0";
const E2E_RUN_ID =
  process.env.E2E_RUN_ID ?? `${Date.now().toString(36)}-${process.pid.toString(36)}`;
const EMAIL = `e2e-mcp-${E2E_WORKER}-${E2E_RUN_ID}@multica.ai`;
const NAME = "E2E MCP User";

const AGENT_ID = "11111111-1111-4111-8111-111111111111";
const OTHER_USER_ID = "99999999-9999-4999-8999-999999999999";

interface SetupResult {
  slug: string;
  userId: string;
}

/** Log in via the real API, capture the authed user id, inject the token, and
 *  return the workspace slug + user id so the test can mock an agent owned
 *  (or not) by this exact user. */
async function loginCapturingUser(page: Page): Promise<SetupResult> {
  const api = new TestApiClient();
  const data = await api.login(EMAIL, NAME);
  const userId: string | undefined = data?.user?.id;
  if (!userId) throw new Error("login did not return a user id");
  const workspace = await api.ensureWorkspace(
    `E2E MCP WS ${E2E_WORKER}`,
    `e2e-mcp-${E2E_WORKER}-${E2E_RUN_ID}`,
  );
  await api.markUserOnboarded();
  const token = api.getToken();
  if (!token) throw new Error("login did not return a token");
  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
    localStorage.setItem("multica:chat:isOpen", "false");
  }, token);
  return { slug: workspace.slug, userId };
}

function mockAgent(ownerId: string, workspaceId: string) {
  return {
    id: AGENT_ID,
    workspace_id: workspaceId,
    runtime_id: "22222222-2222-4222-8222-222222222222",
    name: "MCP Test Agent",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    status: "idle",
    max_concurrent_tasks: 1,
    model: "",
    owner_id: ownerId,
    skills: [],
    created_at: "2026-06-30T00:00:00Z",
    updated_at: "2026-06-30T00:00:00Z",
    archived_at: null,
    archived_by: null,
    composio_toolkit_allowlist: [],
  };
}

/** Mock the connected apps overview/catalog + agent list + agent apps write.
 *  When `viewerRole` is `"member"`, the logged-in user is mocked as a plain
 *  member so `canEdit` is false for an agent owned by someone else. */
async function mockApis(
  page: Page,
  ownerId: string,
  viewerUserId: string,
  viewerRole: "owner" | "member" = "owner",
) {
  let agentApps: Array<{ agent_id: string; toolkit_slug: string; account_scope: string; level: string }> = [];
  const captured: { apps?: unknown } = {};

  await page.route(
    (url) => url.pathname === "/api/config",
    async (route) => {
      const response = await route.fetch();
      const body = await response.json();
      await route.fulfill({
        response,
        json: { ...body, feature_flags: { ...body.feature_flags, composio_mcp_apps: true } },
      });
    },
  );

  if (viewerRole === "member") {
    await page.route(
      (url) => /^\/api\/workspaces\/[^/]+\/members$/.test(url.pathname),
      async (route) => {
        const response = await route.fetch();
        const members = (await response.json()) as Array<{ user_id: string; role: string }>;
        await route.fulfill({
          response,
          json: members.map((m) => (m.user_id === viewerUserId ? { ...m, role: "member" } : m)),
        });
      },
    );
  }

  await page.route(
    (url) => url.pathname === "/api/apps",
    (route) =>
      route.fulfill({
        json: {
          catalog_mode: "all",
          can_manage: viewerRole === "owner",
          team: [],
          personal: [
            {
              id: "conn-notion",
              toolkit_slug: "notion",
              status: "active",
              connected_at: "2026-06-30T00:00:00Z",
              last_used_at: null,
            },
            {
              id: "conn-slack",
              toolkit_slug: "slack",
              status: "active",
              connected_at: "2026-06-30T00:00:00Z",
              last_used_at: null,
            },
          ],
          agent_apps: agentApps,
        },
      }),
  );

  await page.route(
    (url) => url.pathname === "/api/apps/catalog",
    (route) =>
      route.fulfill({
        json: {
          catalog_mode: "all",
          apps: [
            { slug: "notion", name: "Notion", curated: true, recommended_scope: "personal" },
            { slug: "slack", name: "Slack", curated: true, recommended_scope: "personal" },
          ],
        },
      }),
  );

  await page.route(
    (url) => url.pathname === `/api/agents/${AGENT_ID}/apps`,
    (route) => {
      const req = route.request();
      if (req.method() === "PUT") {
        const body = (req.postDataJSON?.() ?? {}) as {
          apps?: Array<{ toolkit_slug: string; account_scope: string; level: string }>;
        };
        captured.apps = body.apps;
        agentApps = (body.apps ?? []).map((app) => ({ ...app, agent_id: AGENT_ID }));
        return route.fulfill({ json: { apps: agentApps } });
      }
      return route.fulfill({ json: { apps: agentApps } });
    },
  );

  await page.route("**/api/agents**", (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const workspaceId = url.searchParams.get("workspace_id") ?? "ws-mock";

    if (req.method() === "GET" && url.pathname.endsWith("/api/agents")) {
      return route.fulfill({
        json: [mockAgent(ownerId, workspaceId)],
      });
    }

    if (req.method() === "GET" && url.pathname.endsWith(`/api/agents/${AGENT_ID}`)) {
      return route.fulfill({
        json: mockAgent(ownerId, workspaceId),
      });
    }

    return route.fallback();
  });

  return () => captured.apps;
}

test.describe("Agent Apps management (editor-only)", () => {
  test("creator can manage agent apps and toggling an app writes the config", async ({
    page,
  }) => {
    const { slug, userId } = await loginCapturingUser(page);
    const getApps = await mockApis(page, userId, userId, "owner");

    await page.goto(`/${slug}/agents/${AGENT_ID}`, {
      waitUntil: "domcontentloaded",
    });
    await waitForPageText(page, "MCP Test Agent");

    const appsSection = page.locator(`#agent-apps-${AGENT_ID}`);
    await expect(appsSection).toBeVisible({ timeout: 15000 });
    await appsSection.getByRole("button", { name: "Manage" }).click();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText("Notion")).toBeVisible();
    await expect(dialog.getByText("Slack")).toBeVisible();

    // Enable Notion → PUT /api/agents/<id>/apps carries the new app entry.
    await dialog.getByRole("switch", { name: "Enable Notion" }).click();
    await expect.poll(() => getApps()).toEqual([
      { toolkit_slug: "notion", account_scope: "personal", level: "write" },
    ]);
  });

  test("a non-editor viewer sees Apps read-only and cannot open Apps settings", async ({ page }) => {
    const { slug, userId } = await loginCapturingUser(page);
    // Agent owned by someone else and viewer is a plain member → read-only Apps section.
    await mockApis(page, OTHER_USER_ID, userId, "member");

    await page.goto(`/${slug}/agents/${AGENT_ID}`, {
      waitUntil: "domcontentloaded",
    });
    await waitForPageText(page, "MCP Test Agent");

    const appsSection = page.locator(`#agent-apps-${AGENT_ID}`);
    await expect(appsSection).toBeVisible({ timeout: 15000 });
    await expect(appsSection.getByRole("button", { name: "Manage" })).toHaveCount(0);
    await expect(appsSection.getByRole("button", { name: "Add" })).toHaveCount(0);

    await page.getByRole("button", { name: "Settings" }).first().click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Apps", exact: true })).toHaveCount(0);
  });
});
