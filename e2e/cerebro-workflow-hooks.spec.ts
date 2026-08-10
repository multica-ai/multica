import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { createTestApi, loginAsDefault } from "./helpers";

const FAMILY_ID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa";
const LIVE_ID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb";
const DRAFT_ID = "cccccccc-cccc-cccc-cccc-cccccccccccc";
const NEW_FAMILY_ID = "dddddddd-dddd-dddd-dddd-dddddddddddd";
const NEW_DRAFT_ID = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee";
const EVENT_MATCH_ID = "ffffffff-ffff-ffff-ffff-ffffffffffff";
const EVENT_NO_MATCH_ID = "11111111-1111-1111-1111-111111111111";
const AGENT_ID = "22222222-2222-2222-2222-222222222222";
const EVAL_ID = "33333333-3333-3333-3333-333333333333";
const SHA = process.env.GITHUB_SHA ?? process.env.CI_COMMIT_SHA ?? "local-candidate";
const SHOT_DIR = `e2e/screenshots/fir-3321/${SHA}`;

const WIDTHS = [
  { name: "320", width: 320, height: 720 },
  { name: "375", width: 375, height: 812 },
  { name: "768", width: 768, height: 1024 },
  { name: "desktop", width: 1280, height: 900 },
] as const;

function transportPolicy(overrides: Record<string, unknown> = {}) {
  return {
    id: DRAFT_ID,
    family_id: FAMILY_ID,
    draft_series_id: "series-1",
    revision: 2,
    version: 5,
    name: "Require a next step",
    description: "No agent stops without a registered continuation",
    contract_rule: "Agents must register a next step before stopping.",
    contract_satisfy: "Register a continuation before stopping.",
    mode: "dry_run",
    fail_mode: "closed",
    events: ["before.task.complete"],
    bindings: [{ kind: "workspace", id: "" }],
    condition_mode: "all",
    conditions: [
      { field: "issue.status", op: "not_in", values: ["done", "cancelled"] },
      { field: "continuation", op: "not_exists" },
      { field: "attempt", op: "lt", value: 3 },
    ],
    handlers: [{
      id: "primary",
      decision: "block",
      requirement: "Choose a valid continuation before completing the task",
      actions: [{ type: "audit.record", config: { event: "continuation_required" } }],
    }],
    observed_run_count: 1,
    pass_count_7d: 18,
    block_count_7d: 3,
    can_publish: true,
    updated_at: "2026-07-15T08:00:00Z",
    last_run_at: "2026-07-15T08:00:00Z",
    lifecycle: {
      state: "live_with_draft",
      live_policy_id: LIVE_ID,
      live_version: 4,
      draft_id: DRAFT_ID,
      draft_series_id: "series-1",
      draft_revision: 2,
      live_unchanged_by_draft: true,
    },
    compatible_events: [
      {
        id: EVENT_NO_MATCH_ID,
        event_id: "seed-no-match",
        event_type: "before.task.complete",
        schema_version: 1,
        occurred_at: "2026-07-15T07:00:00Z",
        expires_at: "2026-07-22T07:00:00Z",
      },
      {
        id: EVENT_MATCH_ID,
        event_id: "seed-match",
        event_type: "before.task.complete",
        schema_version: 1,
        occurred_at: "2026-07-15T08:00:00Z",
        expires_at: "2026-07-22T08:00:00Z",
      },
    ],
    ...overrides,
  };
}

function runRecord(overrides: Record<string, unknown> = {}) {
  return {
    id: "44444444-4444-4444-4444-444444444444",
    policy_id: DRAFT_ID,
    policy_version: 5,
    event: { event_id: "seed-match", event_type: "before.task.complete" },
    source_scope: { kind: "workspace", id: "" },
    result: {
      decision: "allow",
      would_decision: "block",
      requirements: ["Choose a valid continuation"],
      matched_conditions: ["issue.status not_in done,cancelled"],
      matches: [{ policy_id: DRAFT_ID, version: 5, handler_id: "primary", source_scope: { kind: "workspace", id: "" }, decision: "block", dry_run: true }],
      action_results: [{ type: "audit.record", status: "would_run" }],
    },
    latency_ms: 12,
    created_at: "2026-07-15T08:00:00Z",
    ...overrides,
  };
}

test.describe("FIR-3321 Workflow Hooks Live/Draft delivery", () => {
  test.beforeAll(() => mkdirSync(SHOT_DIR, { recursive: true }));

  test.beforeEach(async ({ page }) => {
    const api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_workflows", true);
    await api.setWorkspaceFeatureFlag("cerebro_workflow_hooks", true);
    await mockHookAPI(page);
    await loginAsDefault(page);
  });

  for (const viewport of WIDTHS) {
    test(`lifecycle walk at ${viewport.name} (${viewport.width})`, async ({ page }, testInfo) => {
      const slug = "e2e-workspace";
      await page.setViewportSize({ width: viewport.width, height: viewport.height });

      await page.goto(`/${slug}/workflows/hooks/new`);
      await expect(page.getByRole("heading", { name: "Start with a proven recipe" })).toBeVisible();
      await expectNoHorizontalScroll(page);
      await page.getByRole("button", { name: /Require a continuation before completion/ }).click();

      if (viewport.width < 768) {
        const steps = page.getByRole("list", { name: "Hook steps" });
        await expect(steps.getByRole("button")).toHaveCount(3);
        await page.getByRole("button", { name: "Go to When step" }).click();
        await expect(page.getByRole("region", { name: "when configuration" })).toBeVisible();
        await page.getByRole("button", { name: "Next step" }).click();
        await expect(page.getByRole("region", { name: "guide configuration" })).toBeVisible();
        await page.getByRole("button", { name: "Next step" }).click();
        await expect(page.getByRole("region", { name: "actions configuration" })).toBeVisible();
        await page.getByRole("button", { name: "Back one step" }).click();
        await expect(page.getByRole("region", { name: "guide configuration" })).toBeVisible();
      } else {
        await expect(page.getByRole("list", { name: "Hook chain" })).toBeVisible();
        await page.getByRole("button", { name: "Configure When" }).click();
        // The All/Any conjunction only applies once a second condition exists.
        await page.getByRole("button", { name: "Add filter" }).click();
        await expect(page.getByRole("radio", { name: "All conditions" })).toBeVisible();
        await expect(page.getByRole("radio", { name: "Any condition" })).toBeVisible();
        await page.getByRole("button", { name: "Remove filter 2" }).click();
        await expect(page.getByRole("radio", { name: "All conditions" })).toHaveCount(0);
      }

      await expect(page.getByRole("dialog", { name: "Save as draft?" })).toHaveCount(0);
      const save = page.getByRole("button", { name: "Save draft" });
      await expectClickable(page, save);
      await save.click();
      await expect(page).toHaveURL(new RegExp(`/workflows/hooks/${NEW_FAMILY_ID}$`));
      await expect(page.getByText("Dry run")).toBeVisible();

      await page.getByRole("button", { name: "Test" }).click();
      await expect(page.getByRole("region", { name: "Test and history" })).toBeVisible();
      await expect(page.getByText("Nothing is changed while testing.")).toBeVisible();
      await expect(page.getByText("No compatible events from the last 7 days yet. Save the Draft and let the workflow observe one first.")).toBeVisible();

      // event → no-match → matching Test → Publish → Disable on Live+Draft family
      await page.goto(`/${slug}/workflows/hooks/${FAMILY_ID}`);
      await expect(page.getByLabel("What is enforcing")).toContainText("Live v4 keeps running on every matching event until someone publishes this Draft.");
      await page.getByRole("button", { name: "Test" }).click();
      await expect(page.getByRole("region", { name: "Test and history" })).toBeVisible();
      await page.getByLabel("Past event").click();
      await page.getByRole("option", { name: /Before task completes/ }).first().click();
      await page.getByRole("button", { name: "Run test with selected event" }).click();
      await expect(page.getByText("Dry run · changed nothing")).toBeVisible();

      await page.getByLabel("Past event").click();
      await page.getByRole("option").nth(1).click();
      await page.getByRole("button", { name: "Run test with selected event" }).click();
      await expect(page.getByText(/Would have: Stopped the action/)).toBeVisible();

      const publish = page.getByRole("button", { name: "Publish" });
      await expectClickable(page, publish);
      await publish.click();
      const publishDialog = page.getByRole("dialog", { name: "Publish hook" });
      await expect(publishDialog).toContainText("Publishing replaces Live v4 with Draft v5. Live v4 stays active until this finishes.");
      await publishDialog.getByRole("button", { name: "Confirm publish" }).click();
      await expect(publishDialog).toBeHidden();
      await expect(page.getByText("Enforced", { exact: true })).toBeVisible();

      await page.getByRole("button", { name: "More hook actions" }).click();
      await page.getByRole("menuitem", { name: "Disable hook" }).click();
      const disableDialog = page.getByRole("dialog", { name: "Disable hook?" });
      await expect(disableDialog).toContainText(/stops running on matching events/);
      await disableDialog.getByRole("button", { name: "Confirm disable" }).click();

      // Live → Draft → Discard (Draft kept after Disable; Live pointer remains)
      await page.goto(`/${slug}/workflows/hooks/${FAMILY_ID}`);
      await page.getByRole("button", { name: "More hook actions" }).click();
      await page.getByRole("menuitem", { name: "Discard draft" }).click();
      const discardDialog = page.getByRole("dialog", { name: "Discard draft?" });
      await expect(discardDialog).toContainText(/Live v\d+ remains active\./);
      await discardDialog.getByRole("button", { name: "Confirm discard" }).click();

      await expectNoHorizontalScroll(page);
      await capture(page, testInfo, `lifecycle-${viewport.name}`);
    });
  }

  test("typed actions and family list states remain available on desktop", async ({ page }, testInfo) => {
    const slug = "e2e-workspace";
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto(`/${slug}/workflows/hooks`);
    await expect(page.getByRole("heading", { name: "Hooks", exact: true })).toBeVisible();
    // A family-backed hook shows its lifecycle state, not its raw mode.
    for (const state of ["Off", "Draft", "Enforced", "Enforced · Draft changes", "Managed"]) {
      await expect(page.getByText(state, { exact: true }).first()).toBeVisible();
    }
    await expect(page.getByText("18 passed").first()).toBeVisible();
    await expect(page.getByText("3 blocked").first()).toBeVisible();
    await capture(page, testInfo, "overview-desktop");

    await page.goto(`/${slug}/workflows/hooks/new`);
    await page.getByRole("button", { name: /Start from scratch/ }).click();
    await page.getByRole("button", { name: "Configure Actions" }).click();
    await page.getByRole("button", { name: "Add action", exact: true }).click();
    await page.getByLabel("Action 1 type").click();
    await expect(page.getByRole("option", { name: "Instruct an agent" })).toBeVisible();
    await expect(page.getByRole("option", { name: "Run skill" })).toBeVisible();
    await expect(page.getByRole("option", { name: "Judge gate" })).toBeVisible();
    await page.getByRole("option", { name: "Notify member" }).click();
    await expect(page.getByLabel("Member")).toBeVisible();
  });

  test("readable contracts persist from the editor to the hooks overview", async ({ page }) => {
    const slug = "e2e-workspace";
    await page.goto(`/${slug}/workflows/hooks/${FAMILY_ID}`);

    await expect(page.getByRole("textbox", { name: "Rule" })).toHaveValue("Agents must register a next step before stopping.");
    await page.getByRole("textbox", { name: "Rule" }).fill("Runs must leave a visible next step.");
    await page.getByRole("textbox", { name: "How to satisfy it" }).fill("Register a continuation before stopping.");
    await page.getByRole("button", { name: "Save draft" }).click();

    await page.goto(`/${slug}/workflows/hooks`);
    const card = page.getByRole("button", { name: /Require a next step Runs must leave a visible next step/ });
    await expect(card).toBeVisible();
    await expect(card).toContainText("Register a continuation before stopping.");
  });
});

async function expectClickable(page: Page, locator: ReturnType<Page["getByRole"]>) {
  await expect(locator).toBeVisible();
  await expect(locator).toBeEnabled();
  await expect(locator).toBeInViewport();
  await locator.evaluate((element) => {
    const point = element.getBoundingClientRect();
    const top = document.elementFromPoint(point.left + point.width / 2, point.top + point.height / 2);
    if (top !== element && !element.contains(top)) throw new Error("Control is covered and cannot be clicked");
  });
}

async function expectNoHorizontalScroll(page: Page) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
}

async function capture(page: Page, testInfo: TestInfo, name: string) {
  const path = `${SHOT_DIR}/${name}.png`;
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(100);
  await page.screenshot({ path, fullPage: false, animations: "disabled" });
  await testInfo.attach(name, { path, contentType: "image/png" });
}

// Routes are registered on the browser context, not the page: page-level routes
// stopped intercepting after the editor's client-side navigation (Save and
// Publish both push a new route), so the refetch that follows hit the real
// backend, 404'd and rendered the "Hook could not be read" guard mid-walk.
async function mockHookAPI(page: Page) {
  let familyDraft = transportPolicy();
  let liveOnly = transportPolicy({
    id: LIVE_ID,
    revision: undefined,
    version: 4,
    mode: "enforce",
    can_publish: false,
    lifecycle: {
      state: "live",
      live_policy_id: LIVE_ID,
      live_version: 4,
      live_unchanged_by_draft: false,
    },
    compatible_events: [],
  });
  let newDraft: ReturnType<typeof transportPolicy> | null = null;
  let runs = [runRecord()];
  let discarded = false;

  await page.context().route("**/api/agents?**", (route) => route.fulfill({
    json: [{ id: AGENT_ID, name: "Lone", model: "codex", archived_at: null }],
  }));
  await page.context().route("**/api/skills?**", (route) => route.fulfill({
    json: [{ id: "55555555-5555-5555-5555-555555555555", name: "verification-before-completion", description: "Verify behavior" }],
  }));
  await page.context().route("**/api/cerebro/evals", (route) => route.fulfill({
    json: { evals: [{ id: EVAL_ID, title: "Answer quality", status: "active" }] },
  }));

  await page.context().route("**/api/cerebro/workflow-hooks**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();

    if (path.endsWith(`/${NEW_FAMILY_ID}/runs`) || path.endsWith(`/${NEW_DRAFT_ID}/runs`)) {
      return route.fulfill({ json: { runs: newDraft ? runs : [] } });
    }
    if ((path.endsWith(`/${NEW_FAMILY_ID}/test`) || path.endsWith(`/${NEW_DRAFT_ID}/test`)) && method === "POST") {
      if (newDraft) {
        newDraft = {
          ...newDraft,
          observed_run_count: Math.max(1, Number(newDraft.observed_run_count ?? 0)),
          can_publish: true,
        };
      }
      return route.fulfill({
        json: { side_effects: false, tested_revision: newDraft?.revision ?? 1, retained_event_id: EVENT_MATCH_ID, result: runRecord().result },
      });
    }
    if ((path.endsWith(`/${NEW_FAMILY_ID}/publish`) || path.endsWith(`/${NEW_DRAFT_ID}/publish`)) && method === "POST") {
      if (newDraft) {
        newDraft = {
          ...newDraft,
          id: NEW_DRAFT_ID,
          mode: "enforce",
          version: Number(newDraft.version ?? 1) + 1,
          lifecycle: {
            state: "live",
            live_policy_id: NEW_DRAFT_ID,
            live_version: Number(newDraft.version ?? 1) + 1,
            live_unchanged_by_draft: false,
          },
          compatible_events: [],
        };
      }
      return route.fulfill({ json: newDraft });
    }
    if ((path.endsWith(`/${NEW_FAMILY_ID}`) || path.endsWith(`/${NEW_DRAFT_ID}`)) && method === "PUT") {
      const body = request.postDataJSON();
      newDraft = {
        ...(newDraft ?? transportPolicy({ id: NEW_DRAFT_ID, family_id: NEW_FAMILY_ID })),
        ...body,
        id: NEW_DRAFT_ID,
        family_id: NEW_FAMILY_ID,
        version: Number(newDraft?.version ?? 0) + 1,
        revision: Number(newDraft?.revision ?? 0) + 1,
        mode: "dry_run",
        lifecycle: {
          state: "live_with_draft",
          live_policy_id: LIVE_ID,
          live_version: 4,
          draft_id: NEW_DRAFT_ID,
          draft_series_id: "series-new",
          draft_revision: Number(newDraft?.revision ?? 0) + 1,
          live_unchanged_by_draft: true,
        },
      };
      return route.fulfill({ json: newDraft });
    }
    if (path.endsWith(`/${NEW_FAMILY_ID}`) || path.endsWith(`/${NEW_DRAFT_ID}`)) {
      return route.fulfill({ json: newDraft });
    }

    if (path.endsWith(`/${FAMILY_ID}/runs`) || path.endsWith(`/${DRAFT_ID}/runs`) || path.endsWith(`/${LIVE_ID}/runs`)) {
      return route.fulfill({ json: { runs } });
    }
    if ((path.endsWith(`/${FAMILY_ID}/test`) || path.endsWith(`/${DRAFT_ID}/test`)) && method === "POST") {
      const body = request.postDataJSON() as { event_id?: string };
      const noMatch = body.event_id === EVENT_NO_MATCH_ID;
      const result = noMatch
        ? { decision: "allow", would_decision: "allow", requirements: [], matched_conditions: [], matches: [], action_results: [] }
        : runRecord().result;
      familyDraft = { ...familyDraft, observed_run_count: Math.max(1, Number(familyDraft.observed_run_count ?? 0)), can_publish: !noMatch || familyDraft.can_publish };
      runs = [runRecord({ result })];
      return route.fulfill({
        json: { side_effects: false, tested_revision: familyDraft.revision, retained_event_id: body.event_id, result },
      });
    }
    if ((path.endsWith(`/${FAMILY_ID}/publish`) || path.endsWith(`/${DRAFT_ID}/publish`)) && method === "POST") {
      familyDraft = {
        ...familyDraft,
        id: LIVE_ID,
        mode: "enforce",
        version: 5,
        lifecycle: {
          state: "live",
          live_policy_id: LIVE_ID,
          live_version: 5,
          live_unchanged_by_draft: false,
        },
        compatible_events: [],
      };
      liveOnly = familyDraft;
      return route.fulfill({ json: familyDraft });
    }
    if ((path.endsWith(`/${FAMILY_ID}/disable`) || path.endsWith(`/${DRAFT_ID}/disable`) || path.endsWith(`/${LIVE_ID}/disable`)) && method === "POST") {
      familyDraft = {
        ...familyDraft,
        mode: "off",
        lifecycle: {
          state: "off_with_draft",
          live_policy_id: LIVE_ID,
          live_version: 5,
          draft_id: DRAFT_ID,
          draft_series_id: "series-1",
          draft_revision: 2,
          live_unchanged_by_draft: true,
        },
      };
      return route.fulfill({ json: familyDraft });
    }
    if ((path.endsWith(`/${FAMILY_ID}/draft`) || path.endsWith(`/${DRAFT_ID}/draft`)) && method === "DELETE") {
      discarded = true;
      liveOnly = transportPolicy({
        id: LIVE_ID,
        version: 4,
        mode: "enforce",
        can_publish: false,
        lifecycle: {
          state: "live",
          live_policy_id: LIVE_ID,
          live_version: 4,
          live_unchanged_by_draft: false,
        },
        compatible_events: [],
      });
      return route.fulfill({ json: liveOnly });
    }
    if ((path.endsWith(`/${FAMILY_ID}`) || path.endsWith(`/${DRAFT_ID}`) || path.endsWith(`/${LIVE_ID}`)) && method === "PUT") {
      const body = request.postDataJSON();
      familyDraft = {
        ...familyDraft,
        ...body,
        id: DRAFT_ID,
        family_id: FAMILY_ID,
        mode: "dry_run",
        version: Number(familyDraft.version ?? 4) + 1,
        revision: Number(familyDraft.revision ?? 1) + 1,
        lifecycle: {
          state: "live_with_draft",
          live_policy_id: LIVE_ID,
          live_version: 4,
          draft_id: DRAFT_ID,
          draft_series_id: "series-1",
          draft_revision: Number(familyDraft.revision ?? 1) + 1,
          live_unchanged_by_draft: true,
        },
      };
      return route.fulfill({ json: familyDraft });
    }
    if (path.endsWith(`/${FAMILY_ID}`) || path.endsWith(`/${DRAFT_ID}`) || path.endsWith(`/${LIVE_ID}`)) {
      return route.fulfill({ json: discarded ? liveOnly : familyDraft });
    }

    if (method === "GET" && path.endsWith("/workflow-hooks")) {
      return route.fulfill({
        json: {
          hooks: [
            transportPolicy({
              id: "00000000-0000-0000-0000-000000000001",
              family_id: "00000000-0000-0000-0000-0000000000a1",
              name: "Off policy",
              mode: "off",
              lifecycle: { state: "off", live_unchanged_by_draft: false },
              compatible_events: [],
            }),
            transportPolicy({
              id: "00000000-0000-0000-0000-000000000002",
              family_id: "00000000-0000-0000-0000-0000000000a2",
              name: "Dry run policy",
              mode: "dry_run",
              lifecycle: { state: "draft", draft_id: "00000000-0000-0000-0000-000000000002", draft_revision: 1, live_unchanged_by_draft: false },
              compatible_events: [],
            }),
            discarded ? liveOnly : familyDraft,
            transportPolicy({
              id: "00000000-0000-0000-0000-000000000003",
              family_id: "00000000-0000-0000-0000-0000000000a3",
              name: "Enforced policy",
              mode: "enforce",
              lifecycle: {
                state: "live",
                live_policy_id: "00000000-0000-0000-0000-000000000003",
                live_version: 2,
                live_unchanged_by_draft: false,
              },
              compatible_events: [],
            }),
            transportPolicy({
              id: "00000000-0000-0000-0000-000000000004",
              family_id: "00000000-0000-0000-0000-0000000000a4",
              name: "Managed policy",
              mode: "managed",
              lifecycle: { state: "managed", live_policy_id: "00000000-0000-0000-0000-000000000004", live_version: 1, live_unchanged_by_draft: false },
              compatible_events: [],
            }),
            ...(newDraft ? [newDraft] : []),
          ],
        },
      });
    }
    if (method === "POST" && path.endsWith("/workflow-hooks")) {
      const body = request.postDataJSON();
      newDraft = transportPolicy({
        ...body,
        id: NEW_DRAFT_ID,
        family_id: NEW_FAMILY_ID,
        version: 1,
        revision: 1,
        observed_run_count: 0,
        can_publish: false,
        mode: "dry_run",
        lifecycle: {
          state: "draft",
          draft_id: NEW_DRAFT_ID,
          draft_series_id: "series-new",
          draft_revision: 1,
          live_unchanged_by_draft: false,
        },
        compatible_events: [],
      });
      return route.fulfill({ json: newDraft });
    }
    return route.fulfill({ status: 200, json: familyDraft });
  });
}
