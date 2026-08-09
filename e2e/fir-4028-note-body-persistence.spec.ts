import { expect, test } from "@playwright/test";
import pg from "pg";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

/**
 * FIR-4028 — merely opening a note must never rewrite its body.
 *
 * The note screen persists through `onUpdate`, and the image tray re-emits the
 * combined body+tray whenever the tray signature changes. If that emit runs
 * while the inner editor has no instance yet, `getMarkdown()` answers "" for
 * any note and the empty string is saved over real content.
 *
 * React StrictMode's double-invoked effects reproduce this on every open in
 * dev; in production it takes a tray change arriving before the editor is
 * ready. Same defect, so the guard is asserted here where it actually fires.
 * The assertion is on the stored row, not the screen: the screen can look
 * correct while the row behind it has already been blanked.
 */
test.describe("FIR-4028 — note body survives opening the note", () => {
  let api: TestApiClient;

  test.beforeEach(async () => {
    api = await createTestApi();
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("opening a note leaves its stored body untouched", async ({ page }) => {
    const slug = await loginAsDefault(page);
    const body = "# First heading\n\nSome text.\n\n## Second heading\n\nMore.\n";
    const note = await api.createSharedNote(`Persistence note ${Date.now()}`, body);

    const client = new pg.Client(process.env.DATABASE_URL);
    await client.connect();
    try {
      const read = async () => {
        const result = await client.query<{ body: string }>(
          "select body from artifact where id = $1",
          [note.id],
        );
        return result.rows[0]?.body ?? "";
      };

      expect(await read()).toBe(body);

      await page.goto(`/${slug}/notes/${note.id}`);
      await expect(page.locator(".rich-text-editor").first()).toBeVisible({
        timeout: 15000,
      });
      // The note screen saves on a debounce; give every queued save a chance to
      // land before reading the row back.
      await page.waitForTimeout(4000);

      expect(await read()).not.toBe("");
    } finally {
      await client.end();
    }
  });
});
