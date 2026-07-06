import { test, expect } from "@playwright/test";
import { TestApiClient } from "./fixtures";

/**
 * FIR-2730 — regression guard for the systemic "modal content is taller than
 * the screen, so the top and bottom hang off-screen with NO scrollbar and the
 * user can never reach the buttons" bug.
 *
 * The root cause was that the shared building blocks (`DialogContent` /
 * `SheetContent` in `packages/ui`) owned no height cap and no scroll: a tall
 * dialog rendered centered at its full intrinsic height, pushing its top above
 * the viewport and its bottom below it (measured: top -401px, bottom +401px in
 * a 700px window). The CEREBRO-PATCH(dialog-scroll-default) /
 * CEREBRO-PATCH(sheet-scroll-default) fix makes the default safe: every shared
 * modal caps its height to the viewport and scrolls its overflow.
 *
 * The invariant we lock in, on a deliberately short viewport:
 *   1. The modal panel is fully on-screen vertically (top not above the screen,
 *      bottom not below it) — the exact failure the user reported.
 *   2. When its content is taller than the panel, the panel is genuinely
 *      scrollable (overflow-y is auto/scroll AND scrolling actually moves it),
 *      so the bottom content is reachable rather than clipped.
 *
 * The desktop reminder picker is used as a representative tall modal because it
 * is trivially seedable (one inbox row) and its calendar + time columns always
 * overflow a short viewport. The invariant is about the shared modal shell, not
 * this specific screen.
 */

// Desktop width (so it's the centered Dialog, not the mobile drawer) with a
// deliberately short height so the tall picker cannot fit and MUST scroll.
test.use({ viewport: { width: 1100, height: 340 } });

let api: TestApiClient;

test.afterEach(async () => {
  if (api) await api.cleanup();
});

test("a tall modal stays fully on-screen and scrolls instead of clipping", async ({
  page,
}) => {
  api = new TestApiClient();
  await api.login("e2e@multica.ai", "E2E User");
  const workspace = await api.ensureWorkspace("E2E Workspace", "e2e-workspace");
  await api.resetInboxItems();
  const issue = await api.createIssue("Modal scroll guard");
  await api.insertInboxItem({
    type: "new_comment",
    route: "inbox",
    title: "Modal scroll guard",
    body: "Open the reminder picker on this row",
    issueId: issue.id,
    actorType: "member",
  });

  const token = api.getToken();
  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
  }, token);

  await page.goto(`/${workspace.slug}/inbox`, { waitUntil: "domcontentloaded" });

  const row = page
    .locator('div[role="button"]')
    .filter({ hasText: "Open the reminder picker on this row" })
    .first();
  await row.waitFor({ state: "visible", timeout: 15000 });
  await row.hover();
  await row.getByRole("button", { name: "Remind me" }).click();

  // The shared modal shell — Dialog and Sheet both mark their panel with a
  // data-slot, so this targets whichever one the picker renders.
  const panel = page
    .locator('[data-slot="dialog-content"], [data-slot="sheet-content"]')
    .first();
  await expect(panel).toBeVisible({ timeout: 10000 });

  const metrics = await panel.evaluate((el) => {
    const rect = el.getBoundingClientRect();
    const cs = getComputedStyle(el);
    el.scrollTop = 100000; // try to scroll to the bottom
    return {
      top: rect.top,
      bottom: rect.bottom,
      viewportHeight: window.innerHeight,
      scrollHeight: el.scrollHeight,
      clientHeight: el.clientHeight,
      overflowY: cs.overflowY,
      scrollTopAfter: el.scrollTop,
    };
  });

  // 1. The panel never hangs off the top or bottom of the screen.
  expect(
    metrics.top,
    `modal top hangs above the screen (${metrics.top}px)`,
  ).toBeGreaterThanOrEqual(-1);
  expect(
    metrics.bottom,
    `modal bottom hangs below the screen (${metrics.bottom} > ${metrics.viewportHeight})`,
  ).toBeLessThanOrEqual(metrics.viewportHeight + 1);

  // The short viewport must actually force overflow, otherwise the guard is
  // vacuous.
  expect(
    metrics.scrollHeight,
    "expected the tall picker to overflow the short viewport",
  ).toBeGreaterThan(metrics.clientHeight);

  // 2. Overflow is reachable: the panel scrolls rather than clipping silently.
  expect(["auto", "scroll"]).toContain(metrics.overflowY);
  expect(
    metrics.scrollTopAfter,
    "modal content is clipped — it did not scroll to reveal the bottom",
  ).toBeGreaterThan(0);
});
