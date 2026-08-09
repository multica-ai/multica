import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

// FIR-4139 — the comment box used to be pinned to the bottom of the comments
// rail, so marking a line at the top of a long document put the box you type in
// far away from the text you marked. It now floats next to the marking.
//
// The placement decision itself is unit-tested
// (packages/cerebro-notes/views/note-comments-panel-composer.test.tsx); this
// spec is the whole-path proof: real selection, real toolbar button, real
// geometry, and a comment that actually posts from the floating box.
test.describe("FIR-4139 — the comment box floats next to the marked text", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    // FIR-4028 put the formatting toolbar behind a flag, and this spec drives
    // the toolbar's Comment button. Turn it on for this workspace rather than
    // depending on the release default, which moves.
    await api.setWorkspaceFeatureFlag("cerebro_editor_toolbar", true);
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.setWorkspaceFeatureFlag("cerebro_editor_toolbar", false);
    await api.cleanup();
  });

  test("marking a line near the top opens the composer next to it, and the comment posts", async ({
    page,
  }) => {
    // A long body: the rail's bottom edge is far below the first line, so
    // "floats next to the marking" and "sits at the bottom" are far apart.
    const filler = Array.from(
      { length: 60 },
      (_, i) => `Filler paragraph ${i + 1} for the FIR-4139 geometry check.`,
    ).join("\n\n");
    const doc = await api.createAgentDocument(
      "FIR-4139 floating composer " + Date.now(),
      `Shared policy resolution or the legacy Group gate.\n\n${filler}`,
    );

    await page.goto(`/${api.getWorkspaceSlug()}/documents/${doc.id}`);

    const editor = page.locator('[contenteditable="true"]').first();
    await expect(editor).toBeVisible({ timeout: 15000 });

    // Mark the first line, the way a person does: select it in the body.
    const firstLine = editor.locator("p").first();
    await firstLine.click({ clickCount: 3 });

    await page
      .getByRole("toolbar", { name: "Formatting toolbar" })
      .getByRole("button", { name: "Comment", exact: true })
      .click();

    const composer = page.getByTestId("floating-comment-composer");
    await expect(composer).toBeVisible({ timeout: 15000 });

    // Geometry: the box sits next to the orange marking, not at the bottom of
    // the rail. Both boxes are read from the live layout.
    const marking = page.locator('[data-anchor-id="__draft__"]').first();
    await expect(marking).toBeVisible();
    const markingBox = (await marking.boundingBox())!;
    const composerBox = (await composer.boundingBox())!;
    expect(composerBox.y).toBeGreaterThan(markingBox.y - 40);
    expect(composerBox.y).toBeLessThan(markingBox.y + markingBox.height + 200);

    // And it is a working composer, not just a repositioned shell.
    const body = "Floating composer works — FIR-4139";
    await composer.locator('[contenteditable="true"]').first().click();
    await page.keyboard.type(body);
    // The card holds two "Comment" controls: the mode tab, then the submit
    // button — which stays disabled until the composer has text.
    const post = composer
      .getByRole("button", { name: "Comment", exact: true })
      .last();
    await expect(post).toBeEnabled({ timeout: 10000 });
    await post.click();

    await expect(page.getByText(body)).toBeVisible({ timeout: 15000 });
    // Posting clears the marking, so the box returns to the rail.
    await expect(composer).toBeHidden({ timeout: 15000 });
  });
});
