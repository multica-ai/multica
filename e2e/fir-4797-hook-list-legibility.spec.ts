import { expect, test, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { createTestApi, loginAsDefault } from "./helpers";

const SHA = process.env.GITHUB_SHA ?? process.env.CI_COMMIT_SHA ?? "local-candidate";
const SHOT_DIR = `e2e/screenshots/fir-4797/${SHA}`;

// FIR-4797: the hooks list could not answer "which of these is actually
// running, and which one stopped my agent?" — the filters were one row of
// state pills, a live hook showed its unpublished draft as if it were live,
// and there was no history across hooks. These tests click the real controls.

const SLUG = "e2e-workspace";
const LIVE_FAMILY = "aaaaaaaa-0000-4000-8000-000000000001";
const DRAFTED_FAMILY = "aaaaaaaa-0000-4000-8000-000000000002";
const AGENT_ID = "22222222-2222-2222-2222-222222222222";
const ISSUE_ID = "33333333-3333-3333-3333-333333333333";

function policy(overrides: Record<string, unknown>) {
  return {
    id: "policy-id", family_id: LIVE_FAMILY, version: 1,
    name: "Hook", description: "", contract_rule: "A rule.", contract_satisfy: "Do this.",
    mode: "enforce", fail_mode: "closed", events: ["before.task.complete"],
    bindings: [{ kind: "workspace", id: "" }], condition_mode: "all", conditions: [],
    handlers: [{ id: "primary", decision: "block", requirement: "Register a continuation.", actions: [{ type: "audit.record", config: { event: "x" } }] }],
    observed_run_count: 1, pass_count_7d: 10, block_count_7d: 2, can_publish: false,
    lifecycle: { state: "live", live_policy_id: "policy-id", live_version: 1, live_unchanged_by_draft: false },
    ...overrides,
  };
}

// A live hook carrying an unpublished draft that is missing its action. The
// editor shows the draft's red error; the list must not read it as "broken".
const draftedOverLive = policy({
  id: "draft-id", family_id: DRAFTED_FAMILY, version: 2, revision: 1,
  name: "Message recipient gate", contract_rule: "Address someone.", contract_satisfy: "Mention a recipient.",
  mode: "dry_run", events: ["before.message.send"],
  bindings: [{ kind: "agent", id: AGENT_ID }],
  handlers: [{ id: "primary", decision: "allow", requirement: "", actions: [] }],
  lifecycle: { state: "live_with_draft", live_policy_id: "live-2", live_version: 1, draft_id: "draft-id", draft_revision: 1, live_unchanged_by_draft: true },
  live: { policy_id: "live-2", version: 1, name: "Message recipient gate", decision: "block", requirement: "Mention a recipient.", fail_mode: "closed" },
});

const completionGate = policy({ name: "Completion gate" });

async function mockHooks(page: Page) {
  await page.context().route("**/api/agents?**", (route) => route.fulfill({ json: [{ id: AGENT_ID, name: "Lone", model: "codex", archived_at: null }] }));
  await page.context().route("**/api/issues?**", (route) => route.fulfill({ json: [{ id: ISSUE_ID, title: "Hooks UX", identifier: "FIR-4797" }] }));
  await page.context().route("**/api/cerebro/workflow-hooks**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/workflow-hooks/runs")) {
      return route.fulfill({ json: { runs: [
        { id: "run-1", family_id: DRAFTED_FAMILY, hook_name: "Message recipient gate", policy_version: 1, event: { event_type: "before.message.send", agent_id: AGENT_ID, issue_id: ISSUE_ID }, decision: "block", enforced: true, requirements: ["Mention a recipient."], timed_out: false, latency_ms: 12, created_at: "2026-08-10T10:00:00Z" },
        { id: "run-2", family_id: LIVE_FAMILY, hook_name: "Completion gate", policy_version: 1, event: { event_type: "before.task.complete", agent_id: AGENT_ID, issue_id: ISSUE_ID }, decision: "allow", would_decision: "block", enforced: false, requirements: [], timed_out: false, latency_ms: 4, created_at: "2026-08-10T09:00:00Z" },
      ] } });
    }
    if (path.endsWith("/active-rules")) return route.fulfill({ json: { rules: [] } });
    return route.fulfill({ json: { hooks: [completionGate, draftedOverLive], partial_errors: [] } });
  });
}

test.describe("FIR-4797 hooks list legibility", () => {
  test.beforeAll(() => mkdirSync(SHOT_DIR, { recursive: true }));

  test.beforeEach(async ({ page }) => {
    const api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_workflows", true);
    await api.setWorkspaceFeatureFlag("cerebro_workflow_hooks", true);
    await mockHooks(page);
    await loginAsDefault(page);
  });

  test("the page explains every state, and separates a live hook from its unpublishable draft", async ({ page }) => {
    await page.goto(`/${SLUG}/workflows/hooks`);

    const legend = page.getByLabel("What the states mean");
    await expect(legend).toContainText("It runs on every matching event and can stop, change, or start work.");
    await expect(legend).toContainText("it never stops, changes, or starts anything");
    await expect(legend).toContainText("The edit is saved as a Draft that changes nothing until someone presses");

    const drafted = page.getByRole("button", { name: /Message recipient gate/ });
    // The live version blocks; the draft only guides. The row must show the
    // one that is running.
    await expect(drafted).toContainText("Stop the action");
    await expect(drafted).toContainText("Draft (not live):");
    await expect(drafted).toContainText("Enforced · Draft changes");
    await expect(drafted).toContainText("Draft cannot be published");
  });

  test("filtering by trigger, decision, scope, action, and needs attention actually narrows the list", async ({ page }) => {
    await page.goto(`/${SLUG}/workflows/hooks`);
    const drafted = page.getByRole("button", { name: /Message recipient gate/ });
    const completion = page.getByRole("button", { name: /Completion gate/ });
    await expect(page.getByText("2 of 2")).toBeVisible();

    await page.getByLabel("Trigger").click();
    await page.getByRole("option", { name: "Before message is sent" }).click();
    await expect(drafted).toBeVisible();
    await expect(completion).toBeHidden();

    await page.getByRole("button", { name: "Clear filters" }).click();
    await page.getByLabel("Applies to").click();
    await page.getByRole("option", { name: "Agent" }).click();
    await expect(drafted).toBeVisible();
    await expect(completion).toBeHidden();

    await page.getByRole("button", { name: "Clear filters" }).click();
    await page.getByLabel("Action").click();
    await page.getByRole("option", { name: "Record audit event" }).click();
    await expect(completion).toBeVisible();
    await expect(drafted).toBeHidden();

    await page.getByRole("button", { name: "Clear filters" }).click();
    await page.getByRole("button", { name: /Needs attention/ }).click();
    await expect(drafted).toBeVisible();
    await expect(completion).toBeHidden();
  });

  test("history shows every hook on one timeline, names the agent, and separates a Dry run from a stop", async ({ page }) => {
    await page.goto(`/${SLUG}/workflows/hooks`);
    await page.getByRole("button", { name: "History" }).click();

    await expect(page.getByRole("heading", { name: "Hook history" })).toBeVisible();
    const stopped = page.getByRole("button", { name: /Message recipient gate/ });
    await expect(stopped).toContainText("Lone");
    await expect(stopped).toContainText("Stopped the action");
    await expect(stopped).toContainText("Mention a recipient.");
    await expect(page.getByRole("button", { name: /Completion gate/ })).toContainText("Dry run · changed nothing");

    await page.getByLabel("Search history").fill("completion");
    await expect(page.getByRole("button", { name: /Completion gate/ })).toBeVisible();
    await expect(stopped).toBeHidden();
  });

  test("captures the list and the history for release evidence", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(`/${SLUG}/workflows/hooks`);
    await expect(page.getByLabel("What the states mean")).toBeVisible();
    await page.screenshot({ path: `${SHOT_DIR}/hooks-list.png`, fullPage: true });

    await page.getByRole("button", { name: "History" }).click();
    await expect(page.getByRole("heading", { name: "Hook history" })).toBeVisible();
    await page.screenshot({ path: `${SHOT_DIR}/hooks-history.png`, fullPage: true });
  });

  test("an action can be pointed at the agent that triggered the hook instead of one picked up front", async ({ page }) => {
    await page.goto(`/${SLUG}/workflows/hooks/new`);
    await page.getByRole("button", { name: /Tell the agent to try again instead of stopping it/ }).click();

    // The summary and the Actions step both name the symbolic target rather
    // than one agent chosen when the hook was written.
    await expect(page.getByLabel("What this hook does")).toContainText("The agent that triggered this hook");
    await expect(page.getByRole("button", { name: "Configure Actions" })).toContainText("Instruct an agent — Agent: The agent that triggered this hook");
  });
});
