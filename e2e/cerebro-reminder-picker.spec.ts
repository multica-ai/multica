import { test, expect } from "@playwright/test";
import { TestApiClient } from "./fixtures";

// FIR-2546 — screenshot the desktop reminder picker (calendar + scrollable
// hour/minute columns) to verify it renders and looks like time.rdsx.dev.
let api: TestApiClient;

test.afterEach(async () => {
  if (api) await api.cleanup();
});

test("desktop reminder picker renders calendar + time columns", async ({
  page,
}) => {
  api = new TestApiClient();
  await api.login("e2e@multica.ai", "E2E User");
  const workspace = await api.ensureWorkspace("E2E Workspace", "e2e-workspace");
  await api.resetInboxItems();
  const issue = await api.createIssue("Reminder picker check");
  await api.insertInboxItem({
    type: "new_comment",
    route: "inbox",
    title: "Reminder picker check",
    body: "Open the reminder picker on this row",
    issueId: issue.id,
    actorType: "member",
  });

  const token = api.getToken();
  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
  }, token);

  await page.goto(`/${workspace.slug}/inbox`, { waitUntil: "domcontentloaded" });

  // Hover the row so its action icons (display:none until :group-hover) show,
  // then click the bell ("Remind me") scoped inside the row container.
  const row = page
    .locator('div[role="button"]')
    .filter({ hasText: "Open the reminder picker on this row" })
    .first();
  await row.waitFor({ state: "visible", timeout: 15000 });
  await row.hover();
  await row.getByRole("button", { name: "Remind me" }).click();

  // The custom picker is always visible in the desktop dialog body: a
  // calendar plus scrollable hour (0–23) and minute (5-min step) columns.
  const dialog = page.getByRole("dialog");
  const hours = dialog.getByRole("listbox", { name: "Hour" });
  const minutes = dialog.getByRole("listbox", { name: "Minute" });
  await expect(hours).toBeVisible();
  await expect(minutes).toBeVisible();
  await expect(hours.getByRole("option")).toHaveCount(24);
  await expect(minutes.getByRole("option")).toHaveCount(12);

  // Picking an hour highlights it (drives the reminder value).
  await hours.getByRole("option", { name: "14" }).click();
  await expect(hours.getByRole("option", { name: "14" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
});
