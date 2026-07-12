import { test, expect, type Page } from "@playwright/test";
import { TestApiClient } from "./fixtures";
import { waitForPageText } from "./helpers";

// Stage 3.2 (MUL-3870): the creator-only MCP tab on the agent detail page.
//
// Auth + workspace bootstrap go through the real backend (same as every other
// spec), but the agent list and the Composio connection/catalog endpoints are
// mocked at the network boundary so the test runs without a configured
// COMPOSIO_API_KEY or a live runtime to bind an agent to. The PUT /api/agents
// write is intercepted so we can assert the exact allowlist body the toggle
// produces — the heart of the data contract — instead of depending on the
// backend persisting it.

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

/** Mock the Composio catalog + the current user's active connections (Notion +
 *  Slack), the agent list (owned by `ownerId`), and capture any PUT
 *  /api/agents/<id> body. Returns a getter for the last captured allowlist. */
async function mockApis(page: Page, ownerId: string) {
  const captured: { allowlist?: unknown } = {};

  await page.route("**/api/integrations/composio/toolkits", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify([
        { slug: "notion", name: "Notion", connectable: true },
        { slug: "slack", name: "Slack", connectable: true },
      ]),
    }),
  );

  await page.route("**/api/integrations/composio/connections", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify([
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
      ]),
    }),
  );

  // One handler for both the list (GET, query string) and the write
  // (PUT /api/agents/<id>). Other agent sub-routes fall through.
  await page.route("**/api/agents**", (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const workspaceId = url.searchParams.get("workspace_id") ?? "ws-mock";

    if (req.method() === "PUT" && url.pathname.endsWith(`/api/agents/${AGENT_ID}`)) {
      const body = req.postDataJSON?.() ?? {};
      captured.allowlist = body.composio_toolkit_allowlist;
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          ...mockAgent(ownerId, workspaceId),
          composio_toolkit_allowlist: body.composio_toolkit_allowlist ?? [],
        }),
      });
    }

    if (req.method() === "GET" && url.pathname.endsWith("/api/agents")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify([mockAgent(ownerId, workspaceId)]),
      });
    }

    return route.fallback();
  });

  return () => captured.allowlist;
}

test.describe("Agent MCP tab (creator-only)", () => {
  test("creator sees the MCP Apps tab and toggling a toolkit writes the allowlist", async ({
    page,
  }) => {
    const { slug, userId } = await loginCapturingUser(page);
    const getAllowlist = await mockApis(page, userId);

    await page.goto(`/${slug}/agents/${AGENT_ID}`, {
      waitUntil: "domcontentloaded",
    });
    await waitForPageText(page, "MCP Test Agent");

    // The creator-only tab entry is present and opens the connection list.
    const tab = page.getByRole("button", { name: "MCP Apps" });
    await expect(tab).toBeVisible({ timeout: 15000 });
    await tab.click();

    await expect(page.getByText("Notion")).toBeVisible();
    await expect(page.getByText("Slack")).toBeVisible();

    // Allow Notion → the PUT body carries exactly ["notion"].
    await page.getByLabel(/Allow Notion for this agent/i).click();
    await expect.poll(() => getAllowlist()).toEqual(["notion"]);
  });

  test("a non-creator viewer does not see the MCP Apps tab", async ({ page }) => {
    const { slug } = await loginCapturingUser(page);
    // Agent owned by someone else → the creator gate hides the tab entry.
    await mockApis(page, OTHER_USER_ID);

    await page.goto(`/${slug}/agents/${AGENT_ID}`, {
      waitUntil: "domcontentloaded",
    });
    await waitForPageText(page, "MCP Test Agent");

    // Other tabs render, but the creator-only MCP Apps entry must not.
    await expect(page.getByRole("button", { name: "Activity" })).toBeVisible({
      timeout: 15000,
    });
    await expect(page.getByRole("button", { name: "MCP Apps" })).toHaveCount(0);
  });
});
