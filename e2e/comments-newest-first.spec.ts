import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

// PUL-12: Activity section renders newest root comments first. The composer
// (CommentInput) sits above the timeline so opening a long ticket shows
// fresh activity without scroll, and submitting a new comment makes it
// appear right below the input.
test.describe("Comments — newest-first ordering", () => {
  let api: TestApiClient;

  test.beforeEach(async () => {
    api = await createTestApi();
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("opens long ticket with newest comment visible without scroll", async ({ page }) => {
    const issue = await api.createIssue("PUL-12 long ticket " + Date.now());
    // Seed 10 comments, oldest → newest. The 10th is what should be at the
    // top of the Activity section after the reverse.
    for (let i = 1; i <= 10; i++) {
      await api.createComment(issue.id, `Seeded comment ${i}`);
    }

    const slug = await loginAsDefault(page);
    await page.goto(`/${slug}/issues/${issue.id}`);

    // Wait for issue detail to load.
    await expect(page.locator("text=Properties")).toBeVisible();

    const newest = page.locator("text=Seeded comment 10");
    await expect(newest).toBeVisible();

    // Newest must be in the initial viewport (no scroll). Compare element
    // top against the page's inner height.
    const viewportHeight = await page.evaluate(() => window.innerHeight);
    const box = await newest.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.y).toBeGreaterThanOrEqual(0);
    expect(box!.y).toBeLessThan(viewportHeight);
  });

  test("submitting a new comment shows it at the top with input still in view", async ({ page }) => {
    const issue = await api.createIssue("PUL-12 submit test " + Date.now());
    // A handful of older comments so the timeline has body to push against.
    for (let i = 1; i <= 5; i++) {
      await api.createComment(issue.id, `Older comment ${i}`);
    }

    const slug = await loginAsDefault(page);
    await page.goto(`/${slug}/issues/${issue.id}`);
    await expect(page.locator("text=Properties")).toBeVisible();

    const commentText = "Brand new top comment " + Date.now();
    const commentInput = page.locator(
      'input[placeholder="Leave a comment..."]',
    );
    await expect(commentInput).toBeVisible();
    await commentInput.fill(commentText);
    await page.locator('form button[type="submit"]').last().click();

    // The new comment is rendered.
    const newComment = page.locator(`text=${commentText}`);
    await expect(newComment).toBeVisible();

    // It sits above the older ones in the DOM.
    const newCommentY = (await newComment.boundingBox())!.y;
    const olderY = (await page.locator("text=Older comment 5").boundingBox())!.y;
    expect(newCommentY).toBeLessThan(olderY);

    // The composer is still in the viewport — the user can keep typing.
    const inputBox = await commentInput.boundingBox();
    const viewportHeight = await page.evaluate(() => window.innerHeight);
    expect(inputBox).not.toBeNull();
    expect(inputBox!.y).toBeGreaterThanOrEqual(0);
    expect(inputBox!.y).toBeLessThan(viewportHeight);
  });
});
