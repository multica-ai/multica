import { expect, test } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

// ---------------------------------------------------------------------------
// MUL-4812 — Inbox deep-link lands on its comment, and STAYS there.
//
// These cases cannot live in the jsdom suite: every one of them is a question
// about real layout (where is the row, did a decoded image push it, how many
// rows are actually mounted). jsdom reports zero for all of it.
//
// The image responses are deliberately delayed. Late-decoding images with no
// intrinsic size are the whole reason the calibration hook exists — an image
// that resolves instantly proves nothing, because the anchor would have been
// right without any calibration at all.
// ---------------------------------------------------------------------------

/** 1x1 transparent PNG, rendered large via markdown-adjacent CSS width. */
const PNG_BYTES = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
  "base64",
);

const SLOW_IMAGE_URL = "https://e2e.invalid/slow-image.png";
const IMAGE_DELAY_MS = 1500;

test.describe("Inbox deep-link anchoring", () => {
  let api: TestApiClient;
  let workspaceSlug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    workspaceSlug = await loginAsDefault(page);

    // Serve the image only after the timeline has certainly rendered, so the
    // height it adds always lands *after* the anchor.
    await page.route(SLOW_IMAGE_URL, async (route) => {
      await new Promise((resolve) => setTimeout(resolve, IMAGE_DELAY_MS));
      await route.fulfill({ body: PNG_BYTES, contentType: "image/png" });
    });
  });

  test.afterEach(async () => {
    if (api) await api.cleanup();
  });

  /** True when the element's box sits inside the scroll viewport's box. */
  async function targetIsInViewport(page: import("@playwright/test").Page, commentId: string) {
    return page.evaluate((id) => {
      const el = document.getElementById(`comment-${id}`);
      const container = document.querySelector("[data-tab-scroll-root]");
      if (!el || !container) return false;
      const e = el.getBoundingClientRect();
      const c = container.getBoundingClientRect();
      return e.top >= c.top - 4 && e.top < c.bottom;
    }, commentId);
  }

  async function seedIssueWithComments(count: number, opts: { imageBeforeTarget?: boolean } = {}) {
    const issue = await api.createIssue(`Deep link ${Date.now()}`);
    const ids: string[] = [];
    for (let i = 0; i < count; i++) {
      const body =
        opts.imageBeforeTarget && i === count - 2
          ? `Comment ${i}\n\n![late](${SLOW_IMAGE_URL})`
          : `Comment ${i}`;
      const comment = await api.createComment(issue.id, body);
      ids.push(comment.id);
    }
    return { commentIds: ids, issueId: issue.id };
  }

  test("lands on the deep-linked comment and highlights it", async ({ page }) => {
    const { commentIds, issueId } = await seedIssueWithComments(30);
    const target = commentIds[15]!;
    await api.createInboxComment(issueId, target);

    await page.goto(`/${workspaceSlug}/inbox`, { waitUntil: "domcontentloaded" });

    await expect(page.locator(`#comment-${target}`)).toBeVisible();
    expect(await targetIsInViewport(page, target)).toBe(true);
  });

  // 8a — the case Howard called out: growth ABOVE the target that is not the
  // target's own row and not inside the timeline at all.
  test("holds the anchor when a description image above the target loads late", async ({ page }) => {
    const { commentIds, issueId } = await seedIssueWithComments(30);
    const target = commentIds[15]!;
    await api.updateIssue(issueId, {
      description: `Big picture\n\n![late](${SLOW_IMAGE_URL})`,
    });
    await api.createInboxComment(issueId, target);

    await page.goto(`/${workspaceSlug}/inbox`, { waitUntil: "domcontentloaded" });
    await expect(page.locator(`#comment-${target}`)).toBeVisible();

    // Let the image resolve and the calibration settle.
    await page.waitForTimeout(IMAGE_DELAY_MS + 1500);

    expect(await targetIsInViewport(page, target)).toBe(true);
  });

  // 8b — growth in a sibling row above the target inside the virtualized list.
  test("holds the anchor when the row before the target loads an image late", async ({ page }) => {
    const { commentIds, issueId } = await seedIssueWithComments(30, { imageBeforeTarget: true });
    const target = commentIds[29]!;
    await api.createInboxComment(issueId, target);

    await page.goto(`/${workspaceSlug}/inbox`, { waitUntil: "domcontentloaded" });
    await expect(page.locator(`#comment-${target}`)).toBeVisible();

    await page.waitForTimeout(IMAGE_DELAY_MS + 1500);

    expect(await targetIsInViewport(page, target)).toBe(true);
  });

  test("keeps mounted rows proportional to the viewport, not the timeline", async ({ page }) => {
    // The regression this whole change exists to prevent: a deep-link used to
    // mount every comment. 60 rows must not produce 60 mounted cards.
    const { commentIds, issueId } = await seedIssueWithComments(60);
    const target = commentIds[30]!;
    await api.createInboxComment(issueId, target);

    await page.goto(`/${workspaceSlug}/inbox`, { waitUntil: "domcontentloaded" });
    await expect(page.locator(`#comment-${target}`)).toBeVisible();

    const mounted = await page.locator('[id^="comment-"]').count();
    expect(mounted).toBeLessThan(60);
  });

  test("does not steal the viewport when a newer notification arrives after the user scrolls", async ({
    page,
  }) => {
    const { commentIds, issueId } = await seedIssueWithComments(30);
    const target = commentIds[10]!;
    await api.createInboxComment(issueId, target);

    await page.goto(`/${workspaceSlug}/inbox`, { waitUntil: "domcontentloaded" });
    await expect(page.locator(`#comment-${target}`)).toBeVisible();

    const container = page.locator("[data-tab-scroll-root]");
    await container.hover();
    await page.mouse.wheel(0, 600);
    await page.waitForTimeout(200);
    const afterScroll = await container.evaluate((el) => el.scrollTop);

    // A newer notification lands passively for the issue already on screen.
    const newer = await api.createComment(issueId, "Newer comment");
    await api.createInboxComment(issueId, newer.id, "Newer");
    await page.waitForTimeout(2000);

    // The user is reading. Their position is theirs.
    expect(await container.evaluate((el) => el.scrollTop)).toBe(afterScroll);
  });
});
