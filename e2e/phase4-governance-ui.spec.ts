import "./env";

import { expect, test, type Browser } from "@playwright/test";
import { createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";
import { STARTER_SQUAD_KEYS, api, publishMonthlyReview, sessionOf } from "./phase4-helpers";

// Phase 4 governance UI (spec 4.10, plan 11.21): the /governance route checks. Each
// test starts from its own workspace, set up through the API as the owner, and the
// browser signs in as the same owner. The API scenarios are in
// phase4-governance.spec.ts; these check that the screens show and change what the
// API holds.

const RUN = Date.now().toString(36);
const suffix = () => `${RUN}${Math.random().toString(36).slice(2, 6)}`;

const TAB_NAMES = ["Goals", "Budgets", "Autonomy", "Reviews", "Log", "Controls"];

/** A workspace for one UI check, and the owner's token for the browser. */
async function freshWorkspace(label: string) {
  const client = await createTestApi();
  const id = suffix();
  const workspace = await client.createWorkspace(`Phase 4 UI ${label} ${id}`, `e2e-phase4-ui-${label}-${id}`);
  const runtimeId = await client.seedRuntime(`Phase 4 UI ${label} runtime`, "hermes");
  return { client, workspace, s: sessionOf(client, workspace), runtimeId };
}

/** Open /governance in a fresh browser context signed in as the owner. */
async function openGovernance(browser: Browser, client: TestApiClient, slug: string, tab?: string) {
  const token = client.getToken();
  if (!token) throw new Error("the owner is not signed in");
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
    localStorage.setItem("multica:chat:isOpen", "false");
  }, token);
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(`/${slug}/governance${tab ? `?tab=${tab}` : ""}`, { waitUntil: "domcontentloaded" });
  return { context, page };
}

test("the Governance route renders its six tabs", async ({ browser }) => {
  test.setTimeout(180_000);
  const { client, workspace } = await freshWorkspace("tabs");
  const contexts: { close: () => Promise<void> }[] = [];
  try {
    const view = await openGovernance(browser, client, workspace.slug);
    contexts.push(view.context);
    const tabs = view.page.getByRole("tab");
    await expect(tabs.first()).toBeVisible({ timeout: 45_000 });
    await expect(tabs).toHaveCount(TAB_NAMES.length);
    for (const name of TAB_NAMES) {
      await expect(view.page.getByRole("tab", { name, exact: true })).toBeVisible();
    }
    // Goals is the default tab.
    await expect(view.page.getByRole("tab", { name: "Goals", exact: true })).toHaveAttribute("aria-selected", "true");
  } finally {
    for (const context of contexts) await context.close();
    await client.cleanup();
    await client.deleteWorkspace();
  }
});

test("the Log tab filters its events by kind", async ({ browser }) => {
  test.setTimeout(180_000);
  const { client, workspace, s } = await freshWorkspace("log");
  const contexts: { close: () => Promise<void> }[] = [];
  try {
    // Two kinds of event: a budget is set, and the company is paused.
    await api(s, "POST", "/api/budgets", { scope_type: "company", period: "month", amount_micros: 5_000_000, mode: "warn" });
    await api(s, "PUT", "/api/company/company-state", { state: "paused", stop_running: false });

    const view = await openGovernance(browser, client, workspace.slug, "log");
    contexts.push(view.context);
    const page = view.page;
    // Event kinds are checked in the table's cells, not the filter's own label.
    const cell = (name: string) => page.getByRole("cell", { name, exact: true });
    await expect(cell("Budget set")).toBeVisible({ timeout: 45_000 });
    await expect(cell("Company paused")).toBeVisible();

    // Filter the kind to company paused: the budget event is no longer listed.
    await page.getByRole("combobox", { name: "Kind" }).click();
    await page.getByRole("option", { name: "Company paused", exact: true }).click();
    await expect(cell("Company paused")).toBeVisible();
    await expect(cell("Budget set")).toHaveCount(0);

    // Choosing All shows both again.
    await page.getByRole("combobox", { name: "Kind" }).click();
    await page.getByRole("option", { name: "All", exact: true }).click();
    await expect(cell("Budget set")).toBeVisible();
  } finally {
    for (const context of contexts) await context.close();
    await client.cleanup();
    await client.deleteWorkspace();
  }
});

test("the Controls tab pauses and resumes the company", async ({ browser }) => {
  test.setTimeout(180_000);
  const { client, workspace } = await freshWorkspace("controls");
  const contexts: { close: () => Promise<void> }[] = [];
  try {
    const view = await openGovernance(browser, client, workspace.slug, "controls");
    contexts.push(view.context);
    const page = view.page;
    const state = page.getByRole("group", { name: "Company state" });
    await expect(state).toBeVisible({ timeout: 45_000 });
    await expect(state.getByRole("button", { name: "Running", exact: true })).toHaveAttribute("aria-pressed", "true");

    await page.getByRole("button", { name: "Pause", exact: true }).click();
    await expect(state.getByRole("button", { name: "Paused", exact: true })).toHaveAttribute("aria-pressed", "true");
    await expect(page.getByRole("button", { name: "Resume", exact: true })).toBeVisible();

    await page.getByRole("button", { name: "Resume", exact: true }).click();
    await expect(state.getByRole("button", { name: "Running", exact: true })).toHaveAttribute("aria-pressed", "true");
    await expect(page.getByRole("button", { name: "Pause", exact: true })).toBeVisible();
  } finally {
    for (const context of contexts) await context.close();
    await client.cleanup();
    await client.deleteWorkspace();
  }
});

test("the Goals create dialog creates a goal", async ({ browser }) => {
  test.setTimeout(180_000);
  const { client, workspace, s } = await freshWorkspace("goals");
  const contexts: { close: () => Promise<void> }[] = [];
  try {
    // A key result needs a metric to choose; the first reading creates one.
    await api(s, "POST", "/api/metrics/e2e.ui.signups/readings", {
      scope_type: "company", value: 0, source: "manual", name: "UI signups", direction: "increase",
    });
    const me = await api<{ id: string; name: string }>(s, "GET", "/api/me");

    const view = await openGovernance(browser, client, workspace.slug, "goals");
    contexts.push(view.context);
    const page = view.page;
    await page.getByRole("button", { name: "New goal", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByRole("heading", { name: "New goal" })).toBeVisible();

    await dialog.getByLabel("Title", { exact: true }).fill("Grow signups this quarter");
    await dialog.getByLabel("Starts", { exact: true }).fill(startOfQuarter());
    await dialog.getByLabel("Ends", { exact: true }).fill(endOfQuarter());

    await dialog.getByRole("combobox", { name: "Owner type" }).click();
    await page.getByRole("option", { name: "Member", exact: true }).click();
    await dialog.getByRole("combobox", { name: "Owner", exact: true }).click();
    await page.getByRole("option", { name: me.name, exact: true }).click();

    const keyResults = dialog.getByRole("region", { name: "Key results" });
    await dialog.getByRole("button", { name: "Add key result", exact: true }).click();
    await keyResults.getByLabel("Key result 1", { exact: true }).fill("Signups");
    await keyResults.getByRole("combobox", { name: "Metric 1" }).click();
    await page.getByRole("option", { name: "UI signups", exact: true }).click();
    await keyResults.getByLabel("Start value", { exact: true }).fill("0");
    await keyResults.getByLabel("Target value", { exact: true }).fill("100");

    await dialog.getByRole("button", { name: "Create goal", exact: true }).click();
    await expect(page.getByText("Goal created", { exact: true })).toBeVisible({ timeout: 30_000 });
    await expect(page.getByText("Grow signups this quarter").first()).toBeVisible();
  } finally {
    for (const context of contexts) await context.close();
    await client.cleanup();
    await client.deleteWorkspace();
  }
});

test("the Budgets create dialog creates a company budget", async ({ browser }) => {
  test.setTimeout(180_000);
  const { client, workspace } = await freshWorkspace("budgets");
  const contexts: { close: () => Promise<void> }[] = [];
  try {
    const view = await openGovernance(browser, client, workspace.slug, "budgets");
    contexts.push(view.context);
    const page = view.page;
    await page.getByRole("button", { name: "New budget", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByRole("heading", { name: "New budget" })).toBeVisible({ timeout: 45_000 });

    // The scope defaults to the company and the period to monthly.
    await dialog.getByLabel("Amount per period, in dollars", { exact: true }).fill("25");
    await dialog.getByRole("button", { name: "Create budget", exact: true }).click();
    await expect(page.getByText("Budget created", { exact: true })).toBeVisible({ timeout: 30_000 });
    await expect(page.getByText(/of \$25(\.00)? spent/).first()).toBeVisible();
  } finally {
    for (const context of contexts) await context.close();
    await client.cleanup();
    await client.deleteWorkspace();
  }
});

test("the Autonomy tab changes a squad's level with a reason", async ({ browser }) => {
  test.setTimeout(180_000);
  const { client, workspace, s, runtimeId } = await freshWorkspace("autonomy");
  const contexts: { close: () => Promise<void> }[] = [];
  try {
    const leader = await client.createAgent("Autonomy leader", runtimeId);
    const squad = await client.createSquad("Autonomy squad", leader.id, "Changes its level in the UI.");

    const view = await openGovernance(browser, client, workspace.slug, "autonomy");
    contexts.push(view.context);
    const page = view.page;
    const row = page.getByRole("listitem").filter({ hasText: "Autonomy squad" });
    await expect(row).toBeVisible({ timeout: 45_000 });
    await expect(row.getByText("Still at the default level. No change yet.")).toBeVisible();

    await row.getByRole("combobox", { name: "Level" }).click();
    await page.getByRole("option", { name: "L2 Trusted", exact: true }).click();
    await row.getByLabel("Reason", { exact: true }).fill("Two good months of decisions.");
    await row.getByRole("button", { name: "Save level", exact: true }).click();

    await expect(page.getByText("Autonomy level saved", { exact: true })).toBeVisible({ timeout: 30_000 });
    await expect(row.getByText("L2 Trusted")).toBeVisible();
    await expect(row.getByText("Two good months of decisions.")).toBeVisible();

    // The change is in the governance log with its reason, and the squad carries the level.
    const log = await api<any>(s, "GET", "/api/governance/events?limit=200");
    const change = log.events.find((e: any) => e.kind === "autonomy_changed" && e.subject_id === squad.id);
    expect(change, "the level change is logged").toBeTruthy();
    const level = await api<any>(s, "GET", `/api/squads/${squad.id}/autonomy`);
    expect(level.level).toBe(2);
  } finally {
    for (const context of contexts) await context.close();
    await client.cleanup();
    await client.deleteWorkspace();
  }
});

test("the Reviews tab lists a review published in the API scenario", async ({ browser }) => {
  test.setTimeout(300_000);
  const { client, workspace, s, runtimeId } = await freshWorkspace("reviews");
  const contexts: { close: () => Promise<void> }[] = [];
  try {
    // The same setup as the review scenario in phase4-governance.spec.ts: the starter
    // company's reviewer and the Build squad, then a review published by the reviewer.
    await client.setupCompany();
    await client.applyStarterCompany({ runtime_id: runtimeId, squads: STARTER_SQUAD_KEYS, start: "leave_paused" });
    const build = ((await client.listSquads()) as any[]).find((sq) => sq.name === "Build")!;
    const leaderId = build.leader_id as string;
    const narrative = `Steady output this month; the brief needs a clearer decision line. ${RUN}`;
    await publishMonthlyReview({
      client,
      s,
      workspaceId: workspace.id,
      runtimeId,
      subject: { id: build.id, leaderId },
      instructions: "Report in short bullet points. Name the one decision you need from the boss.",
      narrative,
    });

    const view = await openGovernance(browser, client, workspace.slug, "reviews");
    contexts.push(view.context);
    const page = view.page;
    await expect(page.getByText(narrative)).toBeVisible({ timeout: 45_000 });
    await expect(page.getByText("Meets", { exact: true })).toBeVisible();
    await expect(page.getByText("Instruction change", { exact: true })).toBeVisible();
    await expect(page.getByText("Filed for the boss's decision", { exact: true })).toBeVisible();
  } finally {
    for (const context of contexts) await context.close();
    await client.cleanup();
    await client.deleteWorkspace();
  }
});

/** The first day of the current quarter (UTC), YYYY-MM-DD. */
function startOfQuarter(): string {
  const now = new Date();
  const month = Math.floor(now.getUTCMonth() / 3) * 3;
  return new Date(Date.UTC(now.getUTCFullYear(), month, 1)).toISOString().slice(0, 10);
}

/** The last day of the current quarter (UTC), YYYY-MM-DD. */
function endOfQuarter(): string {
  const now = new Date();
  const month = Math.floor(now.getUTCMonth() / 3) * 3 + 3;
  return new Date(Date.UTC(now.getUTCFullYear(), month, 0)).toISOString().slice(0, 10);
}
