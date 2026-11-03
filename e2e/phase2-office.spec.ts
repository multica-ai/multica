import "./env";

import { expect, test } from "@playwright/test";
import { createTestApi } from "./helpers";

// Phase 2 office, the UI path of spec 2.10 (plan 9.14). Setup goes through the
// API, as in office-first-loop.spec.ts; the checks run on /office.
//
// Covered: a squad's charter tab and its routines list (the operating review the
// first charter save creates, and a cadence routine); the decisions dialog with
// Ask for changes on one proposal and Decide later on another; the brief dialog's
// Chat delivery panel with no chat linked, its empty state.
//
test("a squad's charter and routines show, decisions can be sent back or set aside, and brief delivery shows its empty state", async ({ page }) => {
  test.setTimeout(300_000);
  const api = await createTestApi();
  try {
    const workspace = (await api.getWorkspaces())[0]!;
    const runtimeId = await api.seedRuntime("Phase 2 office runtime");
    const researchLead = await api.createAgent("Phase 2 Research Lead", runtimeId);

    // A floor name unique to this spec: office-first-loop also creates a "Research" floor in the shared run.
    const research = await api.createOfficeFloor("Phase 2 Research");
    const market = await api.createSquad(
      "Market Research",
      researchLead.id,
      "Scans markets and competitors each week.",
    );
    await api.seatOfficeSquads([{ squad_id: market.id, floor_id: research.id, slot: 0 }]);

    const charterMission = "Scan markets and competitors each week; file only evidenced proposals.";
    await api.saveSquadCharter(market.id, {
      mission: charterMission,
      responsibilities: [{ key: "market_scan", text: "Watch markets for changes in the last seven days." }],
    });
    const routine = await api.createSquadRoutine(market.id, {
      purpose: "Weekly market scan",
      cadence: "0 8 * * 1",
    });
    expect(routine.purpose).toBe("Weekly market scan");

    const proposalFields = {
      squad_id: market.id,
      summary: "Competitor pricing moved in the last seven days.",
      evidence: ["https://example.com/pricing-change", "https://example.com/customer-complaints"],
      recommendation: "Run a two-week pilot of the annual plan in one market.",
      options: ["Pilot the annual plan in one market", "Hold the price"],
      cost_risk: "Two weeks of a discounted plan; the risk is margin on the pilot cohort.",
      ask: "Approve the pilot.",
      handoff_squad_id: null,
    };
    const revisable = await api.fileProposal({ ...proposalFields, title: "Pilot the annual plan in Germany" });
    const laterable = await api.fileProposal({ ...proposalFields, title: "Track the competitor price cut" });
    expect(revisable.state).toBe("pending");
    expect(laterable.state).toBe("pending");

    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(`/${workspace.slug}/office`, { waitUntil: "domcontentloaded" });

    // The squad panel opens from its room. Charter first, then the routines on Overview.
    await page.getByRole("tab", { name: /Phase 2 Research/ }).click();
    await page.locator("[data-room-name]", { hasText: "Market Research" }).click();
    await page.getByRole("tab", { name: "Charter" }).click();
    await expect(page.getByText(charterMission)).toBeVisible({ timeout: 45_000 });

    await page.getByRole("tab", { name: "Overview" }).click();
    await expect(page.getByText("Routines", { exact: true })).toBeVisible();
    await expect(page.getByText("Weekly market scan", { exact: true })).toBeVisible();
    const routinesRegion = page.getByRole("region", { name: "Routines" });
    await expect(routinesRegion.getByText("Operating review", { exact: true })).toBeVisible();
    await page.keyboard.press("Escape");

    // Decisions: ask for changes on one, decide later on the other.
    await page.getByRole("button", { name: /^Decisions\b/ }).click();
    const dialog = page.getByRole("dialog", { name: "Decisions" });
    await expect(dialog).toBeVisible();

    await dialog.getByRole("button", { name: revisable.title }).click();
    await dialog.getByLabel("Note (optional)").fill("Narrow it to one market and show the margin.");
    await dialog.getByRole("button", { name: "Ask for changes" }).click();
    await expect(page.getByText(/Asked for changes on/)).toBeVisible();
    await expect
      .poll(async () => (await api.listProposals()).find((p) => p.id === revisable.id)?.state)
      .toBe("revising");

    await dialog.getByRole("button", { name: laterable.title }).click();
    await dialog.getByRole("button", { name: "Decide later" }).click();
    const until = new Date(Date.now() + 5 * 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
    await dialog.getByLabel("Until").fill(until);
    await dialog.getByRole("button", { name: "Set date" }).click();
    await expect(page.getByText(/is set aside until/)).toBeVisible();
    await expect(dialog.getByRole("list", { name: "Decide later" }).getByText(laterable.title)).toBeVisible();
    await page.keyboard.press("Escape");

    // Brief delivery with no chat linked: the panel explains how to link one.
    await page.getByRole("button", { name: /^Brief\b/ }).first().click();
    const briefDialog = page.getByRole("dialog", { name: "Brief" });
    await expect(briefDialog).toBeVisible();
    await briefDialog.getByRole("button", { name: "Chat delivery" }).click();
    const delivery = briefDialog.getByRole("region", { name: "Chat delivery" });
    await expect(delivery.getByRole("link", { name: /Link your Telegram chat first/ })).toBeVisible();
    await expect(delivery.getByRole("combobox")).toHaveCount(0);
  } finally {
    await api.cleanup();
  }
});
