import { test, expect, type Page } from "@playwright/test";
import { TestApiClient } from "./fixtures";
import { waitForPageText } from "./helpers";

// M-96: kimi thinking-effort (thought_level) selection reuses the existing
// provider-generic Thinking picker — no frontend changes were made for this
// feature, on the assumption that `ThinkingPropRow`/`ThinkingPicker` already
// render off `model.thinking.supported_levels` with no provider allowlist
// (see thinking-picker.tsx's own comment: "Claude, Codex, and OpenCode
// today", which is the exact claim this spec checks against a live UI for a
// provider that isn't in that list).
//
// Auth + workspace bootstrap go through the real backend. The runtime,
// agent, and model-discovery endpoints are mocked at the network boundary
// (same approach as agent-mcp.spec.ts) so the test runs without a live kimi
// CLI/daemon bound to this workspace. PUT /api/agents write is intercepted
// so we assert the exact `thinking_level` the picker sends, rather than
// depending on a mocked GET reflecting persisted state after invalidation.

const E2E_WORKER =
  process.env.TEST_PARALLEL_INDEX ?? process.env.TEST_WORKER_INDEX ?? "0";
const E2E_RUN_ID =
  process.env.E2E_RUN_ID ?? `${Date.now().toString(36)}-${process.pid.toString(36)}`;
const EMAIL = `e2e-kimi-thinking-${E2E_WORKER}-${E2E_RUN_ID}@multica.ai`;
const NAME = "E2E Kimi Thinking User";

const AGENT_ID = "33333333-3333-4333-8333-333333333333";
const RUNTIME_ID = "44444444-4444-4444-8444-444444444444";

interface SetupResult {
  slug: string;
  userId: string;
}

/** Log in via the real API, capture the authed user id, inject the token,
 * and return the workspace slug + user id so the mocked agent can be owned
 * by the current user (required for the inspector's edit-permission gate). */
async function loginCapturingUser(page: Page): Promise<SetupResult> {
  const api = new TestApiClient();
  const data = await api.login(EMAIL, NAME);
  const userId: string | undefined = data?.user?.id;
  if (!userId) throw new Error("login did not return a user id");
  const workspace = await api.ensureWorkspace(
    `E2E Kimi Thinking WS ${E2E_WORKER}`,
    `e2e-kimi-thinking-${E2E_WORKER}`,
  );
  const token = api.getToken();
  if (!token) throw new Error("login did not return a token");
  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
    localStorage.setItem("multica:chat:isOpen", "false");
  }, token);
  return { slug: workspace.slug, userId };
}

function mockAgent(ownerId: string, workspaceId: string, thinkingLevel: string) {
  return {
    id: AGENT_ID,
    workspace_id: workspaceId,
    runtime_id: RUNTIME_ID,
    name: "Kimi Thinking Test Agent",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    status: "idle",
    max_concurrent_tasks: 1,
    model: "k3",
    thinking_level: thinkingLevel,
    owner_id: ownerId,
    skills: [],
    created_at: "2026-07-20T00:00:00Z",
    updated_at: "2026-07-20T00:00:00Z",
    archived_at: null,
    archived_by: null,
  };
}

function mockRuntime(ownerId: string, workspaceId: string) {
  return {
    id: RUNTIME_ID,
    workspace_id: workspaceId,
    daemon_id: "daemon-1",
    name: "Kimi Runtime",
    runtime_mode: "local",
    provider: "kimi",
    launch_header: "kimi",
    status: "online",
    device_info: "e2e",
    metadata: {},
    owner_id: ownerId,
    visibility: "private",
    last_seen_at: "2026-07-22T00:00:00Z",
    created_at: "2026-07-20T00:00:00Z",
  };
}

/** Mock the runtime list, agent list/write, and model-discovery endpoints
 * for a single kimi runtime/agent pair. `thinkingLevels` is the discovered
 * `thought_level` catalog attached to the `k3` model. Returns a getter for
 * the last `thinking_level` sent in a PUT /api/agents/<id> body. */
async function mockApis(
  page: Page,
  ownerId: string,
  thinkingLevels: { value: string; label: string }[],
) {
  const captured: { thinkingLevel?: string } = {};

  await page.route("**/api/runtimes**", (route) => {
    const req = route.request();
    if (req.method() !== "GET") return route.fallback();
    const url = new URL(req.url());
    const workspaceId = url.searchParams.get("workspace_id") ?? "ws-mock";
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify([mockRuntime(ownerId, workspaceId)]),
    });
  });

  await page.route(`**/api/runtimes/${RUNTIME_ID}/models`, (route) => {
    const req = route.request();
    if (req.method() !== "POST") return route.fallback();
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        id: "models-req-1",
        runtime_id: RUNTIME_ID,
        status: "completed",
        supported: true,
        models: [
          {
            id: "k3",
            label: "k3",
            default: true,
            thinking:
              thinkingLevels.length > 0
                ? { supported_levels: thinkingLevels, default_level: "on" }
                : undefined,
          },
        ],
        created_at: "2026-07-22T00:00:00Z",
        updated_at: "2026-07-22T00:00:00Z",
      }),
    });
  });

  await page.route("**/api/agents**", (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const workspaceId = url.searchParams.get("workspace_id") ?? "ws-mock";

    if (req.method() === "PUT" && url.pathname.endsWith(`/api/agents/${AGENT_ID}`)) {
      const body = req.postDataJSON?.() ?? {};
      captured.thinkingLevel = body.thinking_level;
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          ...mockAgent(ownerId, workspaceId, body.thinking_level ?? ""),
        }),
      });
    }

    if (req.method() === "GET" && url.pathname.endsWith("/api/agents")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify([mockAgent(ownerId, workspaceId, "")]),
      });
    }

    return route.fallback();
  });

  return () => captured.thinkingLevel;
}

test.describe("Kimi thinking-effort picker (M-96)", () => {
  test("kimi agent's Thinking picker offers the discovered thought_level catalog and persists a pick", async ({
    page,
  }) => {
    const { slug, userId } = await loginCapturingUser(page);
    const getThinkingLevel = await mockApis(page, userId, [
      { value: "on", label: "Thinking On" },
      { value: "max", label: "Max" },
    ]);

    await page.goto(`/${slug}/agents/${AGENT_ID}?view=general`, {
      waitUntil: "domcontentloaded",
    });
    await waitForPageText(page, "Kimi Thinking Test Agent");

    // Not hidden and not stuck on a hardcoded provider allowlist: kimi
    // reaches the same row Claude/Codex/OpenCode use, with no value
    // persisted yet, so it starts on the "no override" sentinel.
    const trigger = page.getByRole("button", { name: /Thinking/i }).last();
    await expect(trigger).toBeVisible({ timeout: 15000 });
    await expect(trigger).toContainText("Follow CLI config");

    await trigger.click();
    await page.getByText("Max", { exact: true }).click();

    // PUT body carries exactly the value the user picked — never
    // normalised across providers.
    await expect.poll(() => getThinkingLevel()).toBe("max");
    await expect(trigger).toContainText("Max");
  });

  test("kimi model advertising a single thought_level option still renders a usable picker", async ({
    page,
  }) => {
    // Regression guard for the real discovery fixture (kimi 0.27.0):
    // session/new's configOptions carries exactly one option
    // (`{"value":"on"}`) for this model. The row must not require 2+
    // options to show — unlike codex's empty-model preview, which is
    // hidden by design (see pickModelEntry in thinking-prop-row.tsx),
    // a real single-option catalog is a legitimate, pickable state.
    const { slug, userId } = await loginCapturingUser(page);
    const getThinkingLevel = await mockApis(page, userId, [
      { value: "on", label: "Thinking On" },
    ]);

    await page.goto(`/${slug}/agents/${AGENT_ID}?view=general`, {
      waitUntil: "domcontentloaded",
    });
    await waitForPageText(page, "Kimi Thinking Test Agent");

    const trigger = page.getByRole("button", { name: /Thinking/i }).last();
    await expect(trigger).toBeVisible({ timeout: 15000 });

    await trigger.click();
    await page.getByText("Thinking On", { exact: true }).click();

    await expect.poll(() => getThinkingLevel()).toBe("on");
  });
});
