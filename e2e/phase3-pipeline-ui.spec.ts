import "./env";

import { expect, test, type Browser } from "@playwright/test";
import { createTestApi } from "./helpers";
import { TestApiClient } from "./fixtures";

// Phase 3 pipeline UI, the /pipeline checks of spec 3.7 (plan 10.15, 10.20).
// Setup goes through the API, as in phase3-company.spec.ts: the Company setup, the
// starter company, and one initiative started directly by the owner. A second
// user joins the workspace as a member. The checks run on /pipeline.
//
// Covered: the board's seven stage columns, the initiative card, the side panel
// that opens from the card, and the Force control, which shows for an owner and is
// absent for a member.
//
const STARTER_SQUAD_KEYS = [
  "executive_office",
  "market_research",
  "opportunity_lab",
  "competitor_intelligence",
  "customer_voice",
  "product_discovery",
  "product_analysis",
  "planning",
  "build",
  "release_ops",
  "growth_gtm",
  "support",
  "finance_risk",
  "people_performance",
];

const STAGE_NAMES = ["Validate", "Define", "Plan", "Build", "Launch", "Operate", "Learn"];
const INITIATIVE_TITLE = "Invoicing for freelancers";

/** Open /pipeline in a fresh browser context signed in as the given user. */
async function openPipeline(browser: Browser, token: string, workspaceSlug: string) {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
    localStorage.setItem("multica:chat:isOpen", "false");
  }, token);
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(`/${workspaceSlug}/pipeline`, { waitUntil: "domcontentloaded" });
  return { context, page };
}

test("the pipeline board shows an initiative, its panel opens, and Force is for admins only", async ({ browser }) => {
  test.setTimeout(300_000);
  const api = await createTestApi();
  const member = new TestApiClient();
  const id = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
  const memberEmail = `e2e-phase3-member-${id}@multica.ai`;
  const contexts: { close: () => Promise<void> }[] = [];
  try {
    const workspace = await api.createWorkspace(`Phase 3 UI ${id}`, `e2e-phase3-ui-${id}`);
    const runtimeId = await api.seedRuntime("Phase 3 UI Hermes runtime", "hermes");
    await api.setupCompany();
    await api.applyStarterCompany({ runtime_id: runtimeId, squads: STARTER_SQUAD_KEYS, start: "leave_paused" });
    const pipeline = (await api.listPipelines()).find((candidate) => candidate.is_default);
    if (!pipeline) throw new Error("the starter company installed no default pipeline");
    await api.createInitiative({
      pipeline_id: pipeline.id,
      title: INITIATIVE_TITLE,
      thesis: "Freelancers chase late payments by hand; a reminder tool could cut that time.",
    });

    // A second user who joins the workspace as a plain member.
    await member.login(memberEmail, "Phase 3 Member");
    await api.addMemberByEmail(memberEmail, "member");
    await member.markUserOnboarded();

    // The member: the board, the card, the panel, and no Force control.
    const memberView = await openPipeline(browser, member.getToken()!, workspace.slug);
    contexts.push(memberView.context);
    const memberPage = memberView.page;

    for (const stage of STAGE_NAMES) {
      await expect(memberPage.getByRole("region", { name: stage })).toBeVisible({ timeout: 45_000 });
    }
    const validate = memberPage.getByRole("region", { name: "Validate" });
    const card = validate.getByRole("button", { name: new RegExp(INITIATIVE_TITLE) });
    await expect(card).toBeVisible();
    await expect(card).toContainText("Product Discovery");
    await expect(card).toContainText("0/2 artifacts");
    await expect(memberPage.getByRole("region", { name: "Define" }).getByText("Nothing in this stage")).toBeVisible();

    await expect(card).toHaveAttribute("aria-pressed", "false");
    await card.click();
    const panel = memberPage.getByRole("dialog");
    await expect(panel.getByRole("heading", { name: "Overview" })).toBeVisible();
    await expect(panel.getByText("Created", { exact: true })).toBeVisible();
    await expect(panel.getByRole("region", { name: "Force move" })).toHaveCount(0);
    await expect(panel.getByRole("button", { name: "Force move" })).toHaveCount(0);

    // The owner: the same panel shows the Force control.
    const ownerView = await openPipeline(browser, api.getToken()!, workspace.slug);
    contexts.push(ownerView.context);
    const ownerPage = ownerView.page;
    await expect(ownerPage.getByRole("region", { name: "Validate" })).toBeVisible({ timeout: 45_000 });
    await ownerPage
      .getByRole("region", { name: "Validate" })
      .getByRole("button", { name: new RegExp(INITIATIVE_TITLE) })
      .click();
    const ownerPanel = ownerPage.getByRole("dialog");
    await expect(ownerPanel.getByRole("heading", { name: "Overview" })).toBeVisible();
    await expect(ownerPanel.getByRole("region", { name: "Force move" })).toBeVisible();
  } finally {
    for (const context of contexts) await context.close();
    await api.cleanup();
    await api.deleteWorkspace();
  }
});
