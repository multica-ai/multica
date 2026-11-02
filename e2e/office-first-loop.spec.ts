import "./env";

import { expect, test } from "@playwright/test";
import { createTestApi } from "./helpers";

// The first loop of the digital company office, driven through the API and
// checked on /office.
//
// Setup repeats what the market_research quickstart does over the API: the
// squad is created first, the server seats it on the first free floor (here the
// Executive floor, the bug the quickstart works around), and the squad is then
// moved to the Research floor's first free slot with PUT /api/office/seats.
//
// Filing: the proposal is filed by the workspace owner acting for the squad, not
// by the squad's leader agent. An agent may file only from inside a running
// daemon task (mayFileProposalForSquad reads the task header), which this suite
// cannot start without a live runtime. The owner path is the one the handler
// allows for an owner or admin.
//
// Handoff: the handoff squad is chosen when the proposal is filed
// (handoff_squad_id). The decision endpoint takes only `decision` and `note`.
//
// The Decisions dialog lists pending proposals only, so the approved proposal
// cannot be the one the UI check sees. A second proposal is filed and left
// pending for that check.
test("an office proposal is filed, approved into a project, and shown on /office", async ({ page }) => {
  test.setTimeout(120_000);
  const api = await createTestApi();
  try {
    const workspace = (await api.getWorkspaces())[0]!;
    const runtimeId = await api.seedRuntime("Office first loop runtime");
    const researchLead = await api.createAgent("Research Lead", runtimeId);
    const deliveryLead = await api.createAgent("Delivery Lead", runtimeId);

    const executive = await api.createOfficeFloor("Executive");
    const research = await api.createOfficeFloor("Research");

    const market = await api.createSquad(
      "Market Research",
      researchLead.id,
      "Scans markets and competitors each week.",
    );
    expect(market.floor_id).toBe(executive.id);
    expect(market.office_slot).toBe(0);

    const moved = await api.seatOfficeSquads([{ squad_id: market.id, floor_id: research.id, slot: 0 }]);
    expect(moved.squads[0]).toMatchObject({ id: market.id, floor_id: research.id, office_slot: 0 });

    const delivery = await api.createSquad("Delivery", deliveryLead.id);

    const proposalFields = {
      squad_id: market.id,
      summary: "Competitor pricing moved in the last seven days; the annual plan gap is the question.",
      evidence: ["https://example.com/pricing-change", "https://example.com/customer-complaints"],
      recommendation: "Run a two-week pilot of the annual plan in one market.",
      options: ["Pilot the annual plan in one market", "Hold the price"],
      cost_risk: "Two weeks of a discounted plan; the risk is margin on the pilot cohort.",
      ask: "Approve the pilot and the delivery squad to run it.",
    };

    const filed = await api.fileProposal({
      ...proposalFields,
      title: "Pilot the annual plan in Germany",
      handoff_squad_id: delivery.id,
    });
    expect(filed.state).toBe("pending");

    const pending = await api.listProposals("pending");
    expect(pending.find((proposal) => proposal.id === filed.id)?.state).toBe("pending");

    const approved = await api.decideProposal(filed.id, "approve", "Go ahead; report the first read in two weeks.");
    expect(approved.state).toBe("approved");
    expect(approved.handoff).toBeDefined();

    const project = await api.getProject(approved.handoff!.project_id);
    expect(project.title).toBe(filed.title);
    const kickoff = await api.getIssue(approved.handoff!.kickoff_issue_id);
    expect(kickoff).toMatchObject({
      project_id: project.id,
      assignee_type: "squad",
      assignee_id: delivery.id,
    });

    const waiting = await api.fileProposal({
      ...proposalFields,
      title: "Track the competitor price cut",
      handoff_squad_id: null,
    });
    expect(waiting.state).toBe("pending");

    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(`/${workspace.slug}/office`, { waitUntil: "domcontentloaded" });

    await page.getByRole("tab", { name: /Research/ }).click();
    await expect(page.getByRole("tab", { name: /Research/ })).toHaveAttribute("aria-selected", "true");
    await expect(page.locator("[data-room-name]", { hasText: "Market Research" })).toBeVisible({
      timeout: 45_000,
    });

    await page.getByRole("button", { name: /^Decisions\b/ }).click();
    const dialog = page.getByRole("dialog", { name: "Decisions" });
    await expect(dialog).toBeVisible();
    const decisions = dialog.getByRole("list", { name: "Decisions" });
    await expect(decisions.getByText(waiting.title)).toBeVisible();
    await expect(decisions.getByText(filed.title)).toHaveCount(0);
  } finally {
    await api.cleanup();
  }
});
