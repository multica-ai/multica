import { test, expect } from "@playwright/test";
import { TestApiClient } from "./fixtures";
import { loginAsDefault, createTestApi } from "./helpers";

/**
 * FIR-4918 — an issue opened from the Dynamic inbox must show the same sidebar
 * fields as the issue page: References and Rounds, with the sidebar open by
 * default in a browser that has never saved a pane layout.
 *
 * The unit test (packages/cerebro-inbox-dynamic/issue-detail-extensions.test.tsx)
 * pins the wiring; this spec drives the real inbox in a real browser, because
 * the reported symptom was visual — References absent from the pane, and the
 * pane collapsed behind an icon.
 */

const API_BASE =
  process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;

const FLAGS = ["cerebro_inbox_dynamic", "cerebro_references", "cerebro_inbox_rounds"];

let api: TestApiClient;

test.beforeEach(async () => {
  api = await createTestApi();
  await api.resetInboxItems();
});

test.afterEach(async () => {
  // Feature flags are workspace-wide rows that api.cleanup() does not track, and
  // the E2E suite shares one database — leaving `cerebro_inbox_dynamic` on would
  // change which inbox every later spec renders.
  for (const flag of FLAGS) await api.setWorkspaceFeatureFlag(flag, false);
  await api.cleanup();
});

test("issue opened from the Dynamic inbox shows References and Rounds", async ({ page }) => {
  for (const flag of FLAGS) await api.setWorkspaceFeatureFlag(flag, true);

  const title = "FIR-4918 inbox field parity";
  const issue = await api.createIssue(title);

  // A real reference, so the section is proved with content and not only by its
  // empty state — this is the field that was missing from the inbox entirely.
  const refRes = await fetch(`${API_BASE}/api/issues/${issue.id}/references`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${api.getToken()}`,
      "X-Workspace-ID": api.getWorkspaceId()!,
    },
    body: JSON.stringify({
      object: "github_pull_request",
      ref_id: "2990",
      label: "PR 2990",
      url: "https://github.com/firtal-group/firtal-cerebro/pull/2990",
    }),
  });
  expect(
    refRes.ok,
    `reference create failed: ${refRes.status} ${await refRes.clone().text()}`,
  ).toBeTruthy();

  await api.insertInboxItem({
    type: "assigned",
    route: "inbox",
    severity: "action_required",
    title,
    issueId: issue.id,
  });

  const slug = await loginAsDefault(page);
  await page.goto(`/${slug}/inbox`, { waitUntil: "domcontentloaded" });

  await page.getByText(title).first().click();

  // The sidebar itself, and both fields inside it, without touching the panel
  // toggle first.
  await expect(page.getByTestId("issue-reference-list")).toBeVisible({ timeout: 20000 });
  await expect(page.getByText("PR 2990").first()).toBeVisible();
  await expect(page.locator('section[aria-label="Rounds"]')).toBeVisible();
  await expect(page.getByText("Properties").first()).toBeVisible();

  await page.screenshot({ path: "/tmp/fir-4918-inbox-references.png", fullPage: false });
});
