/**
 * FIR-3293 — a message that sits in a Round must disappear from the inbox
 * "All messages" box.
 *
 * The first fix filtered All messages on the Round's active_cycle (the snapshot
 * Play takes). A message only enters that snapshot when Play runs AND it is
 * unread and not already running — so any other member kept showing under its
 * Round and in All messages at the same time. The guard is therefore stated in
 * terms of MEMBERSHIP, and the round here deliberately has no Play run.
 */
import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

let api: TestApiClient;
let slug = "";

const ROUND = "All Messages Guard";
const IN_ROUND = "Message that lives in a round";
const NOT_IN_ROUND = "Message with no round";

/** Rounds outlive api.cleanup(), so clear them or stale members leak in. */
async function deleteAllRounds(client: TestApiClient) {
  const res = await (client as any).authedFetch("/api/cerebro/rounds");
  if (!res.ok) return;
  const { rounds = [] } = await res.json();
  for (const r of rounds) {
    await (client as any).authedFetch(`/api/cerebro/rounds/${r.id}`, { method: "DELETE" }).catch(() => {});
  }
}

test.beforeEach(async ({ page }) => {
  api = await createTestApi();
  await api.setWorkspaceFeatureFlag("cerebro_inbox_rounds", true);
  slug = await loginAsDefault(page);
  await deleteAllRounds(api);
  await (api as any).resetInboxItems();
});

test.afterEach(async () => {
  if (!api) return;
  await deleteAllRounds(api);
  await api.cleanup();
});

test("a message in a Round is hidden from All messages even before Play runs", async ({ page }) => {
  const roundIssue = await api.createIssue(IN_ROUND, { allow_duplicate: true });
  const plainIssue = await api.createIssue(NOT_IN_ROUND, { allow_duplicate: true });

  // Both messages land in the inbox; only one of them joins a Round.
  await api.insertInboxItem({ type: "mentioned", route: "inbox", title: IN_ROUND, issueId: roundIssue.id });
  await api.insertInboxItem({ type: "mentioned", route: "inbox", title: NOT_IN_ROUND, issueId: plainIssue.id });

  const created = await (api as any).authedFetch("/api/cerebro/rounds", {
    method: "POST",
    body: JSON.stringify({ name: ROUND }),
  });
  if (!created.ok) throw new Error(`create round failed: ${created.status} ${await created.text()}`);
  const roundId = (await created.json()).id;

  const added = await (api as any).authedFetch(`/api/cerebro/rounds/${roundId}/members`, {
    method: "POST",
    body: JSON.stringify({ issue_id: roundIssue.id }),
  });
  if (!added.ok) throw new Error(`add member failed: ${added.status} ${await added.text()}`);

  // Deliberately no Play: active_cycle stays null, which is exactly the state
  // the old active_cycle-based filter failed to hide.
  await page.goto(`/${slug}/inbox`, { waitUntil: "networkidle" });

  // Scope to the All messages box by its header — the Rounds box legitimately
  // renders the same message, so a page-wide assertion would prove nothing.
  const allBox = page
    .locator("section")
    .filter({ has: page.getByRole("button", { name: "All messages", exact: true }) })
    .first();
  await expect(allBox).toBeVisible();

  await expect(allBox.getByText(NOT_IN_ROUND).first()).toBeVisible();
  await expect(allBox.getByText(IN_ROUND)).toHaveCount(0);
  await page.screenshot({ path: "screenshots/rounds-all-messages.png", fullPage: true });
});
