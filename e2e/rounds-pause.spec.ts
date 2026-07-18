/**
 * FIR-3429 — Pause ends the active Round snapshot and returns unanswered
 * messages to Ready for the next Play.
 */
import { expect, test, type Page } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

let api: TestApiClient;
let slug = "";

const ROUND = "Pause Guard";
const MESSAGE = "Unanswered message returns after Pause";

async function deleteAllRounds(client: TestApiClient) {
  const response = await (client as any).authedFetch("/api/cerebro/rounds");
  if (!response.ok) return;
  const { rounds = [] } = await response.json();
  for (const round of rounds) {
    await (client as any).authedFetch(`/api/cerebro/rounds/${round.id}`, { method: "DELETE" }).catch(() => {});
  }
}

async function showRounds(page: Page) {
  await page.goto(`/${slug}/inbox`, { waitUntil: "networkidle" });
  let manage = page.getByRole("button", { name: "Manage rounds" }).first();
  if (!(await manage.isVisible().catch(() => false))) {
    await page.locator('button[title="Inbox menu"]').click();
    await page.getByRole("menuitem", { name: "Rounds", exact: true }).click();
    manage = page.getByRole("button", { name: "Manage rounds" }).first();
    await expect(manage).toBeVisible();
  }
  const block = page.locator('section[aria-label="Rounds"]').filter({ has: manage }).first();
  const expand = block.getByRole("button", { name: "Expand Rounds" });
  if (await expand.isVisible().catch(() => false)) await expand.click();
  return block;
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

test("Pause folds the Round and returns an unanswered message to the next Play", async ({ page }) => {
  const issue = await api.createIssue(MESSAGE, { allow_duplicate: true });
  await api.insertInboxItem({ type: "mentioned", route: "inbox", title: MESSAGE, issueId: issue.id });

  const created = await (api as any).authedFetch("/api/cerebro/rounds", {
    method: "POST",
    body: JSON.stringify({ name: ROUND }),
  });
  if (!created.ok) throw new Error(`create round failed: ${created.status} ${await created.text()}`);
  const roundId = (await created.json()).id;
  const added = await (api as any).authedFetch(`/api/cerebro/rounds/${roundId}/members`, {
    method: "POST",
    body: JSON.stringify({ issue_id: issue.id }),
  });
  if (!added.ok) throw new Error(`add member failed: ${added.status} ${await added.text()}`);

  const block = await showRounds(page);
  const play = block.getByRole("button", { name: `Play ${ROUND}` });
  await expect(play).toBeVisible();
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith(`/api/cerebro/rounds/${roundId}/start`) && response.request().method() === "POST"),
    play.click(),
  ]);

  const pause = block.getByRole("button", { name: `Pause ${ROUND}` });
  await expect(pause).toBeVisible();
  const message = block.getByText(MESSAGE, { exact: false }).first();
  await expect(message).toBeVisible();
  await Promise.all([
    page.waitForResponse((response) => /\/api\/inbox\/[^/]+\/read$/.test(response.url()) && response.request().method() === "POST"),
    message.click(),
  ]);

  const [pauseResponse] = await Promise.all([
    page.waitForResponse((response) => response.url().endsWith(`/api/cerebro/rounds/${roundId}/pause`) && response.request().method() === "POST"),
    pause.click(),
  ]);
  expect(pauseResponse.status()).toBe(204);
  await expect(block.getByText("Ready to start", { exact: true })).toBeVisible();
  await expect(block.getByRole("button", { name: `Expand ${ROUND}` })).toBeVisible();

  const replay = block.getByRole("button", { name: `Play ${ROUND}` });
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith(`/api/cerebro/rounds/${roundId}/start`) && response.request().method() === "POST"),
    replay.click(),
  ]);
  await expect(block.getByText("1 ready", { exact: true })).toBeVisible();
  await expect(block.getByText(MESSAGE, { exact: false }).first()).toBeVisible();
});

test("answering the final Ready message shows a completed Round with green Play", async ({ page }) => {
  const issue = await api.createIssue(MESSAGE, { allow_duplicate: true });
  await api.insertInboxItem({ type: "mentioned", route: "inbox", title: MESSAGE, issueId: issue.id });

  const created = await (api as any).authedFetch("/api/cerebro/rounds", {
    method: "POST",
    body: JSON.stringify({ name: ROUND }),
  });
  if (!created.ok) throw new Error(`create round failed: ${created.status} ${await created.text()}`);
  const roundId = (await created.json()).id;
  const added = await (api as any).authedFetch(`/api/cerebro/rounds/${roundId}/members`, {
    method: "POST",
    body: JSON.stringify({ issue_id: issue.id }),
  });
  if (!added.ok) throw new Error(`add member failed: ${added.status} ${await added.text()}`);

  const block = await showRounds(page);
  const play = block.getByRole("button", { name: `Play ${ROUND}` });
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith(`/api/cerebro/rounds/${roundId}/start`) && response.request().method() === "POST"),
    play.click(),
  ]);
  await expect(block.getByText("1 ready", { exact: true })).toBeVisible();

  await api.createComment(issue.id, "Handled in this Round");

  const replay = block.getByRole("button", { name: `Play ${ROUND}` });
  await expect(replay).toHaveClass(/bg-success/);
  await expect(block.getByRole("alert")).toHaveText("All ready messages handled.");
  await expect(block.getByText("Complete", { exact: true })).toBeVisible();
});
