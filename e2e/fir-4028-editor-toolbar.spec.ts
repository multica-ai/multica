import { expect, test } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("FIR-4028 — configurable editor toolbar", () => {
  let api: TestApiClient;
  let workspaceSlug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    // The release default stays off until Track C finishes. Every toolbar
    // interaction test turns it on only in its isolated E2E workspace.
    await api.setWorkspaceFeatureFlag("cerebro_editor_toolbar", true);
    // A cold Next.js dev server compiles the dashboard route on the first
    // navigation, which regularly exceeds the helper's 15s default.
    workspaceSlug = await loginAsDefault(page, { workspaceReadyTimeout: 60000 });
  });

  test.afterEach(async ({ request }) => {
    const token = api.getToken();
    if (token) {
      await request.patch("/api/me/preferences", {
        headers: {
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
        },
        data: { cerebro_editor_toolbar_order: null },
      });
    }
    await api.setWorkspaceFeatureFlag("cerebro_editor_toolbar", false);
    await api.cleanup();
  });

  test("the chosen order survives reload and appears above Notes and Documents without a selection", async ({
    page,
  }) => {
    const document = await api.createAgentDocument(
      `Toolbar document ${Date.now()}`,
      "Document body",
    );
    const note = await api.createSharedNote(
      `Toolbar note ${Date.now()}`,
      "Note body",
    );

    await page.goto(`/${workspaceSlug}/settings`);
    await page.getByRole("tab", { name: "Notes", exact: true }).click();
    await expect(page.getByText("Formatting toolbar")).toBeVisible();

    await page.getByRole("button", { name: "Reset" }).click();
    await expect(
      page.getByText("Formatting toolbar updated").last(),
    ).toBeVisible();
    // Slice 5 replaced the per-row Move up / Move down buttons with drag
    // reordering, so dragging Link above Bold is the only persisted path now.
    await page
      .getByTestId("toolbar-setting-link")
      .dragTo(page.getByTestId("toolbar-setting-bold"));
    await expect(
      page.getByText("Formatting toolbar updated").last(),
    ).toBeVisible();

    await page.reload();
    await page.getByRole("tab", { name: "Notes", exact: true }).click();
    const linkSetting = page.getByTestId("toolbar-setting-link");
    await expect(linkSetting).toBeVisible();
    await expect(page.getByTestId("toolbar-setting-bold")).toBeVisible();
    expect(
      await page.locator('[data-testid^="toolbar-setting-"]').evaluateAll(
        (rows) =>
          rows.indexOf(
            document.querySelector('[data-testid="toolbar-setting-link"]')!,
          ) <
          rows.indexOf(
            document.querySelector('[data-testid="toolbar-setting-bold"]')!,
          ),
      ),
    ).toBe(true);

    for (const path of [
      `/${workspaceSlug}/documents/${document.id}`,
      `/${workspaceSlug}/notes/${note.id}`,
    ]) {
      await page.goto(path);
      const toolbar = page.getByRole("toolbar", {
        name: "Formatting toolbar",
      });
      await expect(toolbar).toBeVisible({ timeout: 15000 });
      await expect(toolbar.getByRole("button", { name: "Code" })).toBeVisible();
      await expect(toolbar.getByRole("button", { name: "Link" })).toBeVisible();
      await expect(toolbar.getByRole("button", { name: "Bold" })).toBeVisible();
      expect(
        await toolbar.getByRole("button").evaluateAll(
          (buttons) =>
            buttons.findIndex(
              (button) => button.getAttribute("aria-label") === "Link",
            ) <
            buttons.findIndex(
              (button) => button.getAttribute("aria-label") === "Bold",
            ),
        ),
      ).toBe(true);
      expect(
        await page.evaluate(() => window.getSelection()?.isCollapsed ?? true),
      ).toBe(true);
    }
  });

  // FIR-4028 slice 7 — Outline and References moved out of the layout into one
  // edge-triggered overlay panel, on both surfaces.
  test("the tools panel holds no column until the edge or the shortcut opens it, and carries References", async ({
    page,
  }) => {
    const body = "# First heading\n\nSome text.\n\n## Second heading\n\nMore.\n";
    const document = await api.createAgentDocument(
      `Outline document ${Date.now()}`,
      body,
    );
    const note = await api.createSharedNote(`Outline note ${Date.now()}`, body);

    for (const path of [
      `/${workspaceSlug}/documents/${document.id}`,
      `/${workspaceSlug}/notes/${note.id}`,
    ]) {
      await page.goto(path);
      const edge = page.getByTestId("document-tools-edge");
      await expect(edge).toBeAttached({ timeout: 15000 });

      const panel = page.getByTestId("document-tools-panel");
      await expect(panel).toHaveCount(0);

      await edge.hover({ force: true });
      await expect(panel).toBeVisible();
      await expect(
        panel.getByRole("navigation", { name: "Document outline" }),
      ).toBeVisible();
      await expect(
        panel.getByRole("button", { name: "First heading" }),
      ).toBeVisible();

      // The shortcut pins the panel, so it survives the pointer leaving.
      await page.keyboard.press("ControlOrMeta+Shift+O");
      await page.mouse.move(10, 10);
      await expect(panel).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(panel).toHaveCount(0);
    }

    // References render inside the panel, not in the document body.
    await page.goto(`/${workspaceSlug}/documents/${document.id}`);
    // The shortcut is registered by the document screen, so pressing it before
    // that screen exists does nothing and the panel never opens.
    await expect(page.getByTestId("document-tools-edge")).toBeAttached({
      timeout: 15000,
    });
    await page.keyboard.press("ControlOrMeta+Shift+O");
    const panel = page.getByTestId("document-tools-panel");
    await expect(panel.getByText("References")).toBeVisible({ timeout: 15000 });
  });

  // FIR-4028 slice 8 — the folder is the whole path, and Text size left the row.
  test("the note shows its folder path and no longer spends row width on Text size", async ({
    page,
  }) => {
    const note = await api.createSharedNote(`Folder note ${Date.now()}`, "Body");

    await page.goto(`/${workspaceSlug}/notes/${note.id}`);
    const path = page.getByRole("navigation", { name: "Folder path" });
    await expect(path).toBeVisible({ timeout: 15000 });
    await expect(path.getByRole("button", { name: "All notes" })).toBeVisible();

    await expect(
      page.getByRole("button", { name: "Increase note font size" }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "Decrease note font size" }),
    ).toHaveCount(0);

    await page.getByRole("button", { name: "Note actions" }).click();
    await expect(page.getByText("Text size")).toBeVisible();
  });

  test("right-click on a selection leads with Comment", async ({ page }) => {
    const note = await api.createSharedNote(
      `Context note ${Date.now()}`,
      "Select these words and right-click them.",
    );

    await page.goto(`/${workspaceSlug}/notes/${note.id}`);
    const body = page.locator(".rich-text-editor").first();
    await expect(body).toBeVisible({ timeout: 15000 });

    // Put the caret in the editor first: the very first pointer press after the
    // note opens is spent giving ProseMirror focus and selects nothing.
    await body.getByText("Select these words").first().click();
    // Select a word, then right-click inside the selection.
    await body.getByText("Select these words").first().dblclick();
    await body.getByText("Select these words").first().click({ button: "right" });

    const menu = page.getByRole("menu");
    await expect(menu).toBeVisible();
    await expect(menu.getByText("Comment")).toBeVisible();
    const labels = await menu.getByRole("menuitem").allInnerTexts();
    expect(labels[0]).toContain("Comment");
  });

  test("the active action, hidden action and overflow remain reachable at phone widths", async ({
    page,
  }) => {
    await page.goto(`/${workspaceSlug}/settings`);
    await page.getByRole("tab", { name: "Notes", exact: true }).click();

    // Native drag-and-drop is the persisted reorder path, not a visual-only
    // rearrangement. Put Code before Bold and wait for the one save on drop.
    const codeSetting = page.getByTestId("toolbar-setting-code");
    const boldSetting = page.getByTestId("toolbar-setting-bold");
    await codeSetting.dragTo(boldSetting);
    await expect(page.getByText("Formatting toolbar updated").last()).toBeVisible();
    expect(
      await page.locator('[data-testid^="toolbar-setting-"]').evaluateAll(
        (rows) =>
          rows.indexOf(document.querySelector('[data-testid="toolbar-setting-code"]')!) <
          rows.indexOf(document.querySelector('[data-testid="toolbar-setting-bold"]')!),
      ),
    ).toBe(true);

    await page.getByRole("button", { name: "Hide Code" }).click();
    await expect(page.getByText("Formatting toolbar updated").last()).toBeVisible();

    for (const width of [390, 375]) {
      // A note of its own per width: Bold is a toggle, so reusing one note
      // would leave the second pass switching the mark back off.
      const note = await api.createSharedNote(
        `Responsive toolbar note ${width} ${Date.now()}`,
        "Format this sentence before continuing.",
      );
      await page.setViewportSize({ width, height: 844 });
      await page.goto(`/${workspaceSlug}/notes/${note.id}`);

      const toolbar = page.getByRole("toolbar", { name: "Formatting toolbar" });
      await expect(toolbar).toBeVisible({ timeout: 15000 });
      await expect(toolbar.locator('[data-toolbar-slot="code"]')).not.toBeVisible();

      const body = page.locator(".rich-text-editor").first();
      // The first press after the note opens only gives ProseMirror focus, so
      // selecting has to come after it or the toggle acts on nothing.
      await body.getByText("Format this sentence").click();
      await body.getByText("Format this sentence").click({ clickCount: 3 });
      const bold = toolbar.getByRole("button", { name: "Bold", exact: true });
      await bold.click();
      await expect(bold).toHaveAttribute("aria-pressed", "true");
      // The controls animate their background, so a value read the instant
      // after the interaction is a frame of the transition, not the state.
      // Settle on the value that stops changing.
      const settledBackground = async (locator: typeof bold) => {
        let previous = "";
        await expect
          .poll(async () => {
            const current = await locator.evaluate(
              (element) => getComputedStyle(element).backgroundColor,
            );
            const stable = current === previous && current !== "oklab(0 0 0 / 0)";
            previous = current;
            return stable;
          })
          .toBe(true);
        return previous;
      };

      const activeBackground = await settledBackground(bold);
      const italic = toolbar.getByRole("button", { name: "Italic", exact: true });
      await italic.hover();
      const hoverBackground = await settledBackground(italic);
      expect(activeBackground).not.toBe(hoverBackground);

      await toolbar.getByRole("button", { name: "More formatting" }).click();
      // Matched on the action rather than the label: the desktop menu appends
      // the shortcut to the item ("Code ⌘E") and the keyboard-docked sheet does
      // not, and both are correct. `data-overflow-action` is what both set.
      await expect(
        page.locator('[data-overflow-action="code"]'),
      ).toBeVisible();

      // Every visible control is fully inside the row itself, not merely inside
      // the viewport: a control that spills past the row is overlapped by the
      // note body below it and cannot be clicked at all.
      const overflowing = await toolbar.evaluate((row) => {
        const rowBox = row.getBoundingClientRect();
        return Array.from(row.querySelectorAll("button"))
          .map((button) => ({
            label: button.getAttribute("aria-label") ?? "",
            box: button.getBoundingClientRect(),
          }))
          .filter(({ box }) => box.width > 0 && box.height > 0)
          .filter(
            ({ box }) => box.left < rowBox.left || box.right > rowBox.right,
          )
          .map(({ label }) => label);
      });
      expect(overflowing).toEqual([]);
    }
  });

  // FIR-4028 design review, findings 1 and 3 — the row belongs above the
  // document and has to survive scrolling it. Both were reported as fixed while
  // neither held on a real screen, which is why the check lives here and not in
  // a component test.
  test("the row sits above the note title and stays on screen while the note scrolls", async ({
    page,
  }) => {
    const longBody = Array.from(
      { length: 120 },
      (_, i) => `Paragraph ${i + 1} of a note long enough to scroll.`,
    ).join("\n\n");
    const note = await api.createSharedNote(
      `Scrolling note ${Date.now()}`,
      longBody,
    );

    await page.goto(`/${workspaceSlug}/notes/${note.id}`);
    const toolbar = page.getByRole("toolbar", { name: "Formatting toolbar" });
    await expect(toolbar).toBeVisible({ timeout: 15000 });

    // DOM order: the row precedes the title, so it reads as a control surface
    // for the whole document rather than a strip inside it.
    const title = page.locator('input[aria-label="Title"]');
    await expect(title).toBeVisible();
    const rowIsFirst = await page.evaluate(() => {
      const row = document.querySelector(
        '[role="toolbar"][aria-label="Formatting toolbar"]',
      );
      const heading = document.querySelector('input[aria-label="Title"]');
      if (!row || !heading) return null;
      return (
        (row.compareDocumentPosition(heading) &
          Node.DOCUMENT_POSITION_FOLLOWING) !==
        0
      );
    });
    expect(rowIsFirst).toBe(true);

    const before = await toolbar.boundingBox();
    await page.locator(".rich-text-editor").first().click();
    await page.mouse.wheel(0, 4000);
    await page.waitForTimeout(300);
    const after = await toolbar.boundingBox();

    await expect(toolbar).toBeVisible();
    expect(before).not.toBeNull();
    expect(after).not.toBeNull();
    // Same y as before the scroll: it never travelled with the text.
    expect(Math.abs((after?.y ?? 0) - (before?.y ?? 0))).toBeLessThan(2);
  });

  test("with the flag off, the row disappears and the selection bubble still formats", async ({
    page,
  }) => {
    const note = await api.createSharedNote(
      `Toolbar off note ${Date.now()}`,
      "Keep this selection formatable when the toolbar is off.",
    );
    await api.setWorkspaceFeatureFlag("cerebro_editor_toolbar", false);

    await page.goto(`/${workspaceSlug}/notes/${note.id}`);
    const body = page.locator(".rich-text-editor").first();
    await expect(body).toBeVisible({ timeout: 15000 });
    await expect(
      page.getByRole("toolbar", { name: "Formatting toolbar" }),
    ).toHaveCount(0);

    await body.getByText("Keep this selection").click({ clickCount: 3 });
    const bubble = page.locator(".bubble-menu");
    await expect(bubble).toBeVisible();
    await bubble.locator("button").first().click();
    await expect(body.locator("strong")).toContainText("Keep this selection");

    // The existing note surface remains usable while the new row is absent.
    await expect(page.getByRole("navigation", { name: "Folder path" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Note actions" })).toBeVisible();
  });
});
