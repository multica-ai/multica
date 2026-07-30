/**
 * FIR-3880 — browser contract for the two things the issue asked for:
 *
 *   1. The select-mode action bar offers "Select all after this", so splitting
 *      a long thread is one click instead of a checkbox per comment.
 *   2. The move MOVES the comments. Nothing is copied and no "Moved to new
 *      thread" breadcrumb is left behind at the old location.
 *
 * The Go tests in server/internal/cerebro/comments/move_to_thread_db_test.go
 * already prove the re-parenting at the handler level. This spec is the missing
 * half: that the button in the running app actually drives that handler and
 * that the user sees the promised result.
 *
 * The strongest available "moved, not copied" assertion is comparing the exact
 * set of comment ids before and after. A copy would add ids; a breadcrumb would
 * add a row. Identical id sets can only mean the original rows were re-parented.
 */
import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

const BREADCRUMB_TEXT = "Moved to new thread";

test.describe("FIR-3880 move comments to a new thread", () => {
  let api: TestApiClient;

  test.beforeEach(async () => {
    api = await createTestApi();
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test('"Select all after this" picks the anchor and everything below it, and the move leaves no copy or breadcrumb', async ({
    page,
  }) => {
    const stamp = Date.now();
    const issue = await api.createIssue(`FIR-3880 move-to-thread ${stamp}`);

    // A root comment with three replies. The move is anchored on reply B, so
    // "Select all after this" must add exactly C — proving it reads the thread
    // from the anchor down rather than selecting the whole thread.
    const rootText = `root ${stamp}`;
    const replyAText = `reply A ${stamp}`;
    const replyBText = `reply B ${stamp}`;
    const replyCText = `reply C ${stamp}`;

    // The API serializes created_at at second precision and the thread is
    // sorted on that string, so comments posted inside the same second have no
    // defined order. Space them out to pin the thread order this test asserts.
    const root = await api.createComment(issue.id, rootText);
    await new Promise((r) => setTimeout(r, 1100));
    const replyA = await api.createComment(issue.id, replyAText, root.id);
    await new Promise((r) => setTimeout(r, 1100));
    const replyB = await api.createComment(issue.id, replyBText, root.id);
    await new Promise((r) => setTimeout(r, 1100));
    const replyC = await api.createComment(issue.id, replyCText, root.id);

    const idsBefore = (await api.listComments(issue.id)).map((c) => c.id).sort();

    const slug = await loginAsDefault(page);
    await page.goto(`/${slug}/issues/${issue.id}`, { waitUntil: "domcontentloaded" });
    await expect(page.getByText(replyBText)).toBeVisible({ timeout: 15000 });

    // The anchor assertions below only mean something if B really is the
    // second-to-last comment on screen, so pin the rendered order first.
    await expect(
      page.getByText(new RegExp(`(root|reply [ABC]) ${stamp}`)),
    ).toHaveText([rootText, replyAText, replyBText, replyCText]);

    // Start the move from reply B. The row has no test id, so anchor on the
    // reply's own text and walk up to its row wrapper.
    const replyBRow = page
      .getByText(replyBText)
      .locator(
        'xpath=ancestor::div[contains(concat(" ", normalize-space(@class), " "), " py-3 ")][1]',
      );
    await replyBRow.getByRole("button").last().click();
    await page.getByRole("menuitem", { name: "Reply in new thread" }).click();

    // Select mode is on and only the anchor is picked.
    await expect(
      page.getByText("Pick the comments to move into a new thread"),
    ).toBeVisible();
    const confirmButton = page.getByRole("button", {
      name: /Move \d+ comments? to new thread/,
    });
    await expect(confirmButton).toHaveText("Move 1 comment to new thread");

    // The feature under test: one click adds every comment below the anchor.
    const selectAllAfter = page.getByRole("button", { name: "Select all after this" });
    await expect(selectAllAfter).toBeVisible();
    await selectAllAfter.click();

    // B + C — not the root and not reply A, which sit above the anchor.
    await expect(confirmButton).toHaveText("Move 2 comments to new thread");
    // Nothing left to add, so the button stops being a no-op and hides itself.
    await expect(selectAllAfter).toBeHidden();

    await confirmButton.click();
    await expect(page.getByText("Moved to a new thread")).toBeVisible({ timeout: 15000 });

    // --- The move actually moved -------------------------------------------
    const after = await api.listComments(issue.id);

    // Identical id sets: no copy was made and no breadcrumb row was added.
    expect(after.map((c) => c.id).sort()).toEqual(idsBefore);

    // Requirement 2, stated directly: nothing anywhere says the comments moved.
    for (const comment of after) {
      expect(comment.content ?? "").not.toContain(BREADCRUMB_TEXT);
    }

    // The oldest pick became the new thread's root; the rest hang under it.
    const byId = new Map(after.map((c) => [c.id, c]));
    expect(byId.get(replyB.id)?.parent_id ?? null).toBeNull();
    expect(byId.get(replyC.id)?.parent_id).toBe(replyB.id);
    // The comment that was not picked still hangs off the original root, so the
    // old thread survived the split.
    expect(byId.get(replyA.id)?.parent_id).toBe(root.id);

    // And the user sees no breadcrumb after a reload either.
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByText(replyBText)).toBeVisible({ timeout: 15000 });
    await expect(page.getByText(BREADCRUMB_TEXT)).toHaveCount(0);
  });
});
