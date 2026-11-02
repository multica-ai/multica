import { expect, test, type Locator, type Page } from "@playwright/test";
import { loginAsDefault } from "./helpers";

/**
 * MUL-8490: on a phone, the open agents search used to overflow its slot and
 * cover Reset (390px) and Display settings too (375px), so a tap on Reset
 * landed on the field. Every header control must keep its own box.
 *
 * The agent list is mocked at the network boundary: the header layout does
 * not depend on real agents, and the spec leaves nothing behind.
 */

// Scope "all" lists every agent, so the owner only has to be well-formed.
const OWNER_ID = "44444444-4444-4444-8444-444444444444";

const AGENTS = [
  { id: "a1a1a1a1-1111-4111-8111-111111111111", name: "PR Reviewer", description: "Reviews pull requests" },
  { id: "b2b2b2b2-2222-4222-8222-222222222222", name: "Docs Bot", description: "Keeps docs in sync" },
];

async function mockAgents(page: Page) {
  await page.route("**/api/agents**", (route) => {
    const req = route.request();
    const url = new URL(req.url());
    if (req.method() !== "GET" || !url.pathname.endsWith("/api/agents")) {
      return route.fallback();
    }
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        AGENTS.map((agent) => ({
          ...agent,
          workspace_id: url.searchParams.get("workspace_id") ?? "ws-mock",
          runtime_id: "33333333-3333-4333-8333-333333333333",
          instructions: "",
          avatar_url: null,
          runtime_mode: "local",
          runtime_config: {},
          custom_args: [],
          visibility: "workspace",
          permission_mode: "private",
          invocation_targets: [],
          status: "idle",
          max_concurrent_tasks: 1,
          model: "claude-opus-4",
          owner_id: OWNER_ID,
          skills: [],
          created_at: "2026-06-30T00:00:00Z",
          updated_at: "2026-06-30T00:00:00Z",
          archived_at: null,
          archived_by: null,
        })),
      ),
    });
  });
}

/** Open the list with one filter already on, the case that widens the header most. */
async function openFilteredList(page: Page, width: number) {
  await page.setViewportSize({ width, height: 760 });
  const slug = await loginAsDefault(page);
  await mockAgents(page);
  await page.evaluate((key) => {
    localStorage.setItem(
      key,
      JSON.stringify({
        state: {
          scope: "all",
          filters: { availability: [], runtimes: [], owners: [], models: ["claude-opus-4"], access: [] },
        },
        version: 2,
      }),
    );
  }, `multica_agents_view:${slug}`);
  await page.goto(`/${slug}/agents`, { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("button", { name: "1 filter" })).toBeVisible({ timeout: 20000 });
}

async function box(locator: Locator) {
  const b = await locator.boundingBox();
  if (!b) throw new Error("Could not measure a header control");
  return b;
}

function overlaps(a: { x: number; width: number }, b: { x: number; width: number }) {
  return a.x < b.x + b.width && b.x < a.x + a.width;
}

for (const width of [375, 390]) {
  test(`keeps search, Reset and Display settings apart at ${width}px`, async ({ page }) => {
    await openFilteredList(page, width);

    await page.getByRole("button", { name: "Search agents" }).click();
    const field = page.getByRole("textbox", { name: "Search agents" });
    await field.fill("review");

    const reset = page.getByRole("button", { name: "Reset" });
    const display = page.getByRole("button", { name: "Display settings" });
    const create = page.getByRole("button", { name: "New agent" });
    const filter = page.getByRole("button", { name: "1 filter" });
    const fieldBox = await box(page.getByRole("group").filter({ has: field }));
    // The field keeps room to read a query.
    expect(fieldBox.width).toBeGreaterThanOrEqual(96);
    for (const other of [filter, reset, display, create]) {
      expect(overlaps(fieldBox, await box(other))).toBe(false);
    }
    expect(overlaps(await box(reset), await box(display))).toBe(false);
    const createBox = await box(create);
    expect(createBox.x + createBox.width).toBeLessThanOrEqual(width);

    // Display settings takes its own click.
    await display.click({ timeout: 3000 });
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);

    // Reset takes its own click and clears both the search and the filter.
    await reset.click({ timeout: 3000 });
    await expect(field).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Filter", exact: true })).toBeVisible();
    await expect(page.getByRole("heading", { level: 1, name: /All agents/ })).toBeVisible();
  });
}

test("keeps the title beside an open search on a wide window", async ({ page }) => {
  await openFilteredList(page, 1280);

  await page.getByRole("button", { name: "Search agents" }).click();
  await page.getByRole("textbox", { name: "Search agents" }).fill("review");

  await expect(page.getByRole("heading", { level: 1, name: /All agents/ })).toBeVisible();
  const fieldBox = await box(
    page.getByRole("group").filter({ has: page.getByRole("textbox", { name: "Search agents" }) }),
  );
  expect(fieldBox.width).toBe(240);
});
