/**
 * Steps shared by the core-path benchmark and the Desktop resource scenarios
 * (MUL-9539, MUL-9612): waiting for an issue's content, and driving the Work
 * panel's Inbox.
 */
import { expect, type Locator, type Page, type Response } from "@playwright/test";
import { settle } from "./frame-metrics";
import type { Manifest, SeededIssue } from "./core-dataset";
import type { Sample } from "./perf-target";

/** The Work panel's Inbox section at its largest limit, twenty rows. */
export function inboxStorage(slug: string): Record<string, string> {
  return { [`multica_sidebar_tree:${slug}`]: JSON.stringify({ state: { inboxSettings: { limit: 20 } }, version: 0 }) };
}

export const detailSelector = '[data-mc="issue-detail"][data-view="page"]';

/**
 * Resolves with page-clock marks once the issue's title and the target comment are on screen.
 *
 * `first-screen`: the first comment is mounted, or the history starts below
 * the first screen (a long description or sub-issue list above it). Virtuoso
 * renders no rows out of view, so a comment below the fold mounts only when
 * the reader scrolls to it — that is the scroll step's job, not the open's.
 */
export async function waitForIssue(page: Page, title: string, commentId: string, mode: "first-screen" | "in-view" | "title") {
  const handle = await page.waitForFunction(({ title, commentId, mode, detailSelector }) => {
    const marks = window.__corePerf.marks;
    const details = [...document.querySelectorAll(detailSelector)];
    const detail = details.find((d) => d.querySelector('[data-mc="issue-title"]')?.textContent?.includes(title));
    if (!detail) return false;
    marks.title ??= performance.now();
    if (mode === "title") return { title: marks.title, content: marks.title };
    const root = detail.querySelector<HTMLElement>("[data-tab-scroll-root]");
    const target = root?.querySelector<HTMLElement>(`[data-comment-content="${commentId}"]`);
    if (mode === "first-screen" && root && !target) {
      const activity = detail.querySelector<HTMLElement>('[data-mc="issue-activity"]');
      if (activity && activity.getBoundingClientRect().top >= root.getBoundingClientRect().bottom) {
        marks.content ??= performance.now();
        return { title: marks.title, content: marks.content };
      }
    }
    if (!root || !target || !target.textContent?.trim()) return false;
    const row = target.closest("[data-virtual-thread-mounted]");
    if (row?.getAttribute("data-virtual-thread-mounted") === "false") return false;
    const box = target.getBoundingClientRect();
    if (box.height === 0) return false;
    if (mode === "in-view") {
      const view = root.getBoundingClientRect();
      if (!(box.bottom > view.top && box.top < view.bottom)) return false;
    }
    marks.content ??= performance.now();
    return { title: marks.title, content: marks.content };
  }, { title, commentId, mode, detailSelector }, { polling: "raf", timeout: 60_000 });
  return (await handle.jsonValue()) as { title: number; content: number };
}

/**
 * Boots the app on the issues list and waits for the Work panel's Inbox rows.
 * `all` first presses "Show N more", mounting every row.
 */
export async function openInbox(sample: Sample, manifest: Manifest, rowsShown: "20" | "all"): Promise<Locator> {
  const { page } = sample;
  const rows = page.locator("li[data-inbox-row]");
  // The API sheds Inbox reads under load (503, MUL-9456) and the panel then
  // shows "No notifications" instead of retrying. Nothing is measured before
  // the rows are there, so boot again rather than fail the run.
  for (let attempt = 1; ; attempt++) {
    let shed = false;
    const onResponse = (response: Response) => {
      if (new URL(response.url()).pathname === "/api/inbox" && response.status() === 503) shed = true;
    };
    page.on("response", onResponse);
    try {
      await sample.open(`/${manifest.workspace.slug}/issues`);
      await expect.poll(async () => shed || (await rows.count()) > 0, { timeout: 60_000 }).toBe(true);
    } finally {
      page.off("response", onResponse);
    }
    if (!shed) break;
    if (attempt === 3) throw new Error("the API shed the Inbox read on three boots in a row");
    console.warn(`the API shed the Inbox read (attempt ${attempt}); booting again`);
  }
  await expect(rows.first()).toBeVisible();
  const firstTitle = manifest.issues[manifest.inbox[0]!.key]!.title;
  await expect(rows.filter({ hasText: firstTitle })).toHaveCount(1);
  if (rowsShown === "all") {
    const showMore = page.getByRole("button", { name: /^Show \d+ more/ }).first();
    const hidden = Number((await showMore.textContent())?.match(/\d+/)?.[0] ?? 0);
    expect(hidden, "the Inbox section hides hundreds of rows").toBeGreaterThan(500);
    await showMore.click();
    // A virtualized list mounts only the rows near the viewport, so the
    // expansion is read from the toggle rather than from the row count.
    await expect(page.getByRole("button", { name: "Show fewer" }).first()).toHaveAttribute("aria-expanded", "true");
  }
  await expect.poll(() => rows.count(), "the Inbox section shows the twenty measured rows").toBeGreaterThanOrEqual(20);
  await settle(page, 2000);
  return rows;
}

/** Clicks the issue's Inbox row and resolves once its content is on screen. */
export async function switchTo(page: Page, rows: Locator, issue: SeededIssue, mode: "in-view" | "title") {
  await page.evaluate(() => { window.__corePerf.marks = {}; });
  await rows.filter({ hasText: issue.title }).locator("a").first().click();
  return waitForIssue(page, issue.title, issue.targetCommentId, mode);
}
