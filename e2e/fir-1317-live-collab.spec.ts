/**
 * FIR-1317 — live co-editing in notes, proven with two real browsers.
 *
 * Two different signed-in people open the SAME note at the same time and both
 * type. The spec asserts the three things the feature promises:
 *   1. presence — each window names the other person as editing the note,
 *   2. relay    — each window receives the other's text while it is typed,
 *   3. survival — the final document in BOTH windows contains BOTH texts.
 *
 * Guard rails that keep this from passing vacuously:
 *   - presence is asserted BEFORE any typing, so a spec whose WebSocket never
 *     connected (useNoteLiveCollab silently retries) fails loudly instead of
 *     falling through to plain autosave and going green,
 *   - the remote caret is asserted to carry the OTHER person's name, which is
 *     the specific thing live editing adds over saving.
 */

import { test, expect, type Page, type BrowserContext } from "@playwright/test";
import { createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

const SECOND_USER_EMAIL = "fir1317-collab-b@multica.ai";
const SECOND_USER_NAME = "Collab Bee";
const FIRST_USER_NAME = "E2E User";

const TEXT_A = "Alpha writes the opening line.";
const TEXT_B = "Bravo writes a second line.";

let api: TestApiClient;

async function openNoteAs(
  context: BrowserContext,
  token: string,
  slug: string,
  noteId: string,
): Promise<Page> {
  await context.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
  }, token);
  const page = await context.newPage();
  await page.goto(`/${slug}/notes/${noteId}`, { waitUntil: "domcontentloaded" });
  // The editor is the note's contenteditable surface; wait for it before the
  // room assertions so a slow first render is not read as a failed connection.
  await expect(page.locator('[contenteditable="true"]').first()).toBeVisible({
    timeout: 30000,
  });
  return page;
}

/** The editor body as one string, used for the convergence assertion. */
async function bodyText(page: Page): Promise<string> {
  return (await page.locator('[contenteditable="true"]').first().innerText()).replace(
    /\s+/g,
    " ",
  );
}

test.describe("FIR-1317 live co-editing", () => {
  test.beforeEach(async () => {
    api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_notes", true);
    await api.setWorkspaceFeatureFlag("cerebro_note_live_collab", true);
  });

  test.afterEach(async () => {
    await api.setWorkspaceFeatureFlag("cerebro_note_live_collab", false);
    await api.cleanup();
  });

  test("two people write in the same note at once and both texts survive", async ({
    browser,
  }) => {
    const slug = api.getWorkspaceSlug();
    const note = await api.createSharedNote("FIR-1317 live co-editing");
    const second = await api.loginSecondaryUser(SECOND_USER_EMAIL, SECOND_USER_NAME);

    const contextA = await browser.newContext();
    const contextB = await browser.newContext();

    try {
      const pageA = await openNoteAs(contextA, api.getToken()!, slug, note.id);
      const pageB = await openNoteAs(contextB, second.token, slug, note.id);

      // 1. Presence. Each window must name the OTHER person. This is the
      //    connection proof — if the room never joined, peers is empty and the
      //    presence line is not rendered at all.
      await expect(
        pageA.getByText(`${SECOND_USER_NAME} is editing this note`),
      ).toBeVisible({ timeout: 30000 });
      await expect(
        pageB.getByText(`${FIRST_USER_NAME} is editing this note`),
      ).toBeVisible({ timeout: 30000 });

      // 2. Relay, A → B. A types; B must receive it without reloading.
      const editorA = pageA.locator('[contenteditable="true"]').first();
      await editorA.click();
      await editorA.pressSequentially(TEXT_A, { delay: 15 });
      await expect(pageB.locator('[contenteditable="true"]').first()).toContainText(
        TEXT_A,
        { timeout: 30000 },
      );

      // 3. Relay, B → A, typed at the END of the shared document while A's own
      //    text is already there — this is the concurrent case, not a handover.
      const editorB = pageB.locator('[contenteditable="true"]').first();
      await editorB.click();
      await pageB.keyboard.press("Control+End");
      await pageB.keyboard.press("Enter");
      await editorB.pressSequentially(TEXT_B, { delay: 15 });
      await expect(pageA.locator('[contenteditable="true"]').first()).toContainText(
        TEXT_B,
        { timeout: 30000 },
      );

      // 4. The caret label carries the other person's name — the visible marker
      //    of who is writing where.
      await expect(pageA.locator(".cerebro-remote-caret")).toContainText(
        SECOND_USER_NAME,
        { timeout: 30000 },
      );

      // 5. Convergence: nobody's text was overwritten. Both windows hold both.
      const finalA = await bodyText(pageA);
      const finalB = await bodyText(pageB);
      expect(finalA).toContain(TEXT_A);
      expect(finalA).toContain(TEXT_B);
      expect(finalB).toContain(TEXT_A);
      expect(finalB).toContain(TEXT_B);

      // Capture both windows while the session is still live, so the picture
      // shows what the assertions above just proved: both texts, the presence
      // line, and the other person's named caret. Reloading first would end the
      // session and drop the caret out of the image.
      await pageA.screenshot({
        path: "test-results/fir-1317-live-collab-window-a.png",
        fullPage: false,
      });
      await pageB.screenshot({
        path: "test-results/fir-1317-live-collab-window-b.png",
        fullPage: false,
      });

      // 6. Live editing must not cost persistence. Both texts have to survive a
      //    reload, because the room's document is only in memory — if autosave
      //    stopped running the note would look perfect and still be lost.
      await pageA.reload({ waitUntil: "domcontentloaded" });
      const reloaded = pageA.locator('[contenteditable="true"]').first();
      await expect(reloaded).toContainText(TEXT_A, { timeout: 30000 });
      await expect(reloaded).toContainText(TEXT_B, { timeout: 30000 });
    } finally {
      await contextA.close();
      await contextB.close();
    }
  });
});
