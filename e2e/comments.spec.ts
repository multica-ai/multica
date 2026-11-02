import { test, expect, type Locator } from "@playwright/test";
import { createTestApi, loginAsDefault, waitForPageText } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Comments", () => {
  let api: TestApiClient;
  let issueId: string;
  let issueTitle: string;
  let workspaceSlug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    issueTitle = "E2E Comment Test " + Date.now();
    const issue = await api.createIssue(issueTitle);
    issueId = issue.id;
    workspaceSlug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("can add a comment on an issue", async ({ page }) => {
    await page.goto(`/${workspaceSlug}/issues/${issueId}`, { waitUntil: "domcontentloaded" });
    await waitForPageText(page, issueTitle);

    // Wait for issue detail to load
    await expect(page.locator("text=Properties")).toBeVisible();

    // The composer renders as a static shell until clicked (readonly-first);
    // clicking it mounts and focuses the real ProseMirror editor.
    const shell = page.getByTestId("comment-composer-shell");
    await expect(shell).toBeVisible();
    await shell.click();

    // Type a comment
    const commentText = "E2E comment " + Date.now();
    const editor = page
      .locator('.ProseMirror[data-placeholder="Leave a comment..."], .ProseMirror:has([data-placeholder="Leave a comment..."])')
      .first();
    await expect(editor).toBeVisible();
    await editor.click({ force: true });
    await editor.fill(commentText);

    await page.keyboard.press("ControlOrMeta+Enter");

    // Comment should appear in the activity section
    await expect(page.locator(`text=${commentText}`)).toBeVisible({
      timeout: 5000,
    });
  });

  test("comment submit button is disabled when empty", async ({ page }) => {
    await page.goto(`/${workspaceSlug}/issues/${issueId}`, { waitUntil: "domcontentloaded" });
    await waitForPageText(page, issueTitle);

    await expect(page.locator("text=Properties")).toBeVisible();

    // Submit button should be disabled when input is empty. The composer is a
    // static shell until clicked (readonly-first) — the disabled state must
    // hold in shell form too, no activation needed.
    const shell = page.getByTestId("comment-composer-shell");
    await expect(shell).toBeVisible();
    const composer = shell.locator("xpath=ancestor::div[contains(@class, 'rounded-lg')][1]");
    const submitBtn = composer.locator("button:has(svg.icon-arrow-up)").last();
    await expect(submitBtn).toBeDisabled();
  });

  // OL-998 — the composers' text areas keep a stable I-beam (host padding
  // included), match the readonly shell before/after activation, and focus the
  // editor when the padding is pressed. Toolbars keep their own cursors.
  test("composer text areas keep the I-beam cursor and focus on padding clicks", async ({ page }) => {
    await api.createComment(issueId, "root comment for reply probes");
    await page.goto(`/${workspaceSlug}/issues/${issueId}`, { waitUntil: "domcontentloaded" });
    await waitForPageText(page, issueTitle);
    // /issues/<uuid> canonicalizes to /issues/<KEY> shortly after load and the
    // composers remount on that (key={id}) — settle the URL before probing.
    await page.waitForURL((u) => !/\/issues\/[0-9a-f-]{36}/.test(u.pathname), { timeout: 15000 });

    const probe = (x: number, y: number) =>
      page.evaluate(
        ({ x, y }) => {
          const el = document.elementFromPoint(x, y) as HTMLElement | null;
          if (!el) return null;
          return {
            cursor: getComputedStyle(el).cursor,
            editable: el.isContentEditable,
            tag: el.tagName.toLowerCase(),
            cls: (el.getAttribute("class") || "").slice(0, 80),
          };
        },
        { x: Math.round(x), y: Math.round(y) },
      );
    const boxOf = async (locator: Locator) => {
      const box = await locator.boundingBox();
      if (!box) throw new Error("element has no bounding box");
      return box;
    };
    const expectTextCursor = async (x: number, y: number, what: string) => {
      const p = await probe(x, y);
      expect(p, `${what}: nothing hit at ${x},${y}`).not.toBeNull();
      expect(
        p!.cursor === "text" || (p!.cursor === "auto" && p!.editable),
        `${what}: expected I-beam, got cursor=${p!.cursor} on <${p!.tag} class="${p!.cls}">`,
      ).toBe(true);
    };

    // --- main composer: shell state ---
    const mainShell = page.getByTestId("comment-composer-shell");
    await expect(mainShell).toBeVisible();
    const shellBox = await boxOf(mainShell);
    await expectTextCursor(shellBox.x + 20, shellBox.y + shellBox.height / 2, "shell text");
    await expectTextCursor(shellBox.x + shellBox.width / 2, shellBox.y + 3, "shell pad-top");

    // --- main composer: active + empty ---
    await mainShell.click();
    const mainEditor = page
      .locator('.ProseMirror[data-placeholder="Leave a comment..."], .ProseMirror:has([data-placeholder="Leave a comment..."])')
      .first();
    await expect(mainEditor).toBeVisible();
    // Tag the ProseMirror element: the placeholder attribute vanishes once text
    // lands, so placeholder-based locators would stop matching mid-test.
    await mainEditor.evaluate((el) => el.setAttribute("data-ol998", "main-pm"));
    const mainPM = page.locator('[data-ol998="main-pm"]');
    // The editor host owns the px-3 py-2 padding around the ProseMirror.
    const mainHost = mainPM.locator('xpath=ancestor::div[contains(@class,"overflow-y-auto")][1]');
    const mainCard = mainPM.locator('xpath=ancestor::div[contains(@class,"rounded-lg")][1]');
    const hostBox = await boxOf(mainHost);
    const cardBox = await boxOf(mainCard);
    const lineY = (await boxOf(mainPM.locator("p").first())).y + 10;

    await expectTextCursor(hostBox.x + 20, lineY, "editor text");
    await expectTextCursor(hostBox.x + hostBox.width / 2, hostBox.y + 3, "host pad-top");
    await expectTextCursor(hostBox.x + hostBox.width / 2, hostBox.y + hostBox.height - 3, "host pad-bottom");
    await expectTextCursor(hostBox.x + 3, lineY, "host pad-left");
    await expectTextCursor(hostBox.x + hostBox.width - 3, lineY, "host pad-right");
    // Same point, shell vs active: the cursor must not flip on activation.
    await expectTextCursor(shellBox.x + shellBox.width / 2, shellBox.y + 3, "pad-top after activation");

    // Toolbar strip: blank stays the arrow, controls keep their affordances.
    const toolbarBlank = await probe(cardBox.x + cardBox.width / 2, cardBox.y + cardBox.height - 5);
    expect(toolbarBlank).not.toBeNull();
    expect(["auto", "default"], `toolbar blank cursor=${toolbarBlank!.cursor}`).toContain(toolbarBlank!.cursor);
    expect(toolbarBlank!.editable).toBe(false);
    const attachBtn = mainCard.locator("button:has(svg.icon-paperclip)").first();
    const attachBox = await boxOf(attachBtn);
    expect((await probe(attachBox.x + attachBox.width / 2, attachBox.y + attachBox.height / 2))!.cursor).toBe("pointer");
    // Disabled send keeps the Button's pointer-events-none passthrough: the hit
    // lands below the button and shows the arrow.
    const sendBtn = mainCard.locator("button:has(svg.icon-arrow-up)").first();
    const sendBox = await boxOf(sendBtn);
    const disabledSend = await probe(sendBox.x + sendBox.width / 2, sendBox.y + sendBox.height / 2);
    expect(disabledSend!.tag, "disabled send should be hit-transparent").not.toBe("button");
    expect(["auto", "default"]).toContain(disabledSend!.cursor);

    // Pressing the host padding focuses the editor: typing lands immediately.
    await page.mouse.click(hostBox.x + hostBox.width / 2, hostBox.y + 3);
    await page.keyboard.type("via padding");
    await expect(mainPM).toContainText("via padding");
    // Enabled send shows the pointer. `disabled` lifts on the debounced
    // onUpdate (100ms), so wait for it instead of racing the debounce.
    await expect(sendBtn).toBeEnabled();
    const sendEnabledBox = await boxOf(sendBtn);
    expect((await probe(sendEnabledBox.x + sendEnabledBox.width / 2, sendEnabledBox.y + sendEnabledBox.height / 2))!.cursor).toBe("pointer");

    // --- reply composer ---
    const replyShell = page.getByTestId("reply-composer-shell").first();
    await replyShell.scrollIntoViewIfNeeded();
    await expect(replyShell).toBeVisible();
    // The text surface owns the vertical padding bands around the input column
    // (it is the py-2.5 owner since OL-998; the shell inside is cursor-text).
    const replyShellBox = await boxOf(replyShell);
    await expectTextCursor(replyShellBox.x + 20, replyShellBox.y + replyShellBox.height / 2, "reply shell text");
    const replySurfaceShell = page.getByTestId("reply-text-surface");
    const replySurfaceShellBox = await boxOf(replySurfaceShell);
    await expectTextCursor(
      replySurfaceShellBox.x + replySurfaceShellBox.width / 2,
      replySurfaceShellBox.y + 3,
      "reply top band (shell)",
    );
    // The avatar keeps its alignment with the first text line (the padding
    // move must not shift it — review #1). Measure the visible avatar div,
    // not the profile-link wrapper around it.
    const replyRow = replySurfaceShell.locator("xpath=..");
    const replyAvatar = replyRow.locator('[data-slot="avatar"]').first();
    const avatarBoxShell = await boxOf(replyAvatar);
    const shellLineBox = await boxOf(replyShell.locator("p").first());
    expect(
      Math.abs(avatarBoxShell.y + avatarBoxShell.height / 2 - (shellLineBox.y + shellLineBox.height / 2)),
      "avatar vs first-line center offset (shell)",
    ).toBeLessThanOrEqual(1);

    await replyShell.click();
    const replyEditor = page
      .locator('.ProseMirror[data-placeholder="Leave a reply..."], .ProseMirror:has([data-placeholder="Leave a reply..."])')
      .first();
    await expect(replyEditor).toBeVisible();
    await replyEditor.evaluate((el) => el.setAttribute("data-ol998", "reply-pm"));
    const replyPM = page.locator('[data-ol998="reply-pm"]');
    const replySurface = page.getByTestId("reply-text-surface");
    // Center the reply box so the sticky main composer cannot overlap the probes.
    await replySurface.evaluate((el) => el.scrollIntoView({ block: "center" }));
    const replyActiveBox = await boxOf(replySurface);
    const replyFirstLineBox = await boxOf(replyPM.locator("p").first());
    const replyLineY = replyFirstLineBox.y + 10;
    await expectTextCursor(replyActiveBox.x + replyActiveBox.width / 2, replyActiveBox.y + 3, "reply top band (active)");
    await expectTextCursor(replyActiveBox.x + replyActiveBox.width / 2, replyActiveBox.y + replyActiveBox.height - 3, "reply bottom band (active)");
    await expectTextCursor(replyActiveBox.x + 20, replyLineY, "reply editor text");
    // Avatar alignment holds after activation too.
    const avatarBoxActive = await boxOf(replyAvatar);
    expect(
      Math.abs(avatarBoxActive.y + avatarBoxActive.height / 2 - (replyFirstLineBox.y + replyFirstLineBox.height / 2)),
      "avatar vs first-line center offset (active)",
    ).toBeLessThanOrEqual(1);

    // Pressing the top band focuses the reply editor: typing lands.
    await page.mouse.click(replyActiveBox.x + replyActiveBox.width / 2, replyActiveBox.y + 3);
    await page.keyboard.type("reply via padding");
    await expect(replyPM).toContainText("reply via padding");

    // With content the toolbar strip (pb-9) appears: blank arrow, buttons pointer.
    const replyColumn = page.getByTestId("reply-text-surface").locator(":scope > div");
    // pb-9 and the send button's enabled state both follow the debounced
    // onUpdate (100ms) — wait for the layout, don't race it.
    await expect(replyColumn).toHaveClass(/pb-9/);
    await replyColumn.evaluate((el) => el.scrollIntoView({ block: "center" }));
    const colBox = await boxOf(replyColumn);
    const stripBlank = await probe(colBox.x + colBox.width / 2, colBox.y + colBox.height - 6);
    expect(stripBlank).not.toBeNull();
    expect(
      ["auto", "default"],
      `reply toolbar blank hit <${stripBlank!.tag} class="${stripBlank!.cls}"> cursor=${stripBlank!.cursor}`,
    ).toContain(stripBlank!.cursor);
    const replyAttachBox = await boxOf(replyColumn.locator("button:has(svg.icon-paperclip)").first());
    expect((await probe(replyAttachBox.x + replyAttachBox.width / 2, replyAttachBox.y + replyAttachBox.height / 2))!.cursor).toBe("pointer");
    const replySendBtn = replyColumn.locator("button:has(svg.icon-arrow-up)").first();
    // `disabled` lifts on the debounced onUpdate (100ms) — wait, don't race it.
    await expect(replySendBtn).toBeEnabled();
    const replySendBox = await boxOf(replySendBtn);
    expect((await probe(replySendBox.x + replySendBox.width / 2, replySendBox.y + replySendBox.height / 2))!.cursor).toBe("pointer");
  });

  // OL-998 review — the send lock (pointer-events-none + dimmed editor host)
  // must hold while a reply is in flight: pressing the dimmed editor must not
  // focus it, typed text must not land, and an accepted send must still clear
  // the box.
  test("reply composer send lock holds against presses mid-send", async ({ page }) => {
    test.setTimeout(120000);
    await api.createComment(issueId, "root comment body for lock test");
    await page.goto(`/${workspaceSlug}/issues/${issueId}`, { waitUntil: "domcontentloaded" });
    await waitForPageText(page, "root comment body for lock test");
    // Canonicalization remounts the composers (key={id}) — settle it first.
    await page.waitForURL((u) => !/\/issues\/[0-9a-f-]{36}/.test(u.pathname), { timeout: 15000 });

    // Stretch the in-flight window so the locked state is observable.
    await page.route("**/api/issues/*/comments", async (route) => {
      if (route.request().method() !== "POST") return route.continue();
      await new Promise((r) => setTimeout(r, 4000));
      return route.continue();
    });

    const shell = page.getByTestId("reply-composer-shell").first();
    await shell.scrollIntoViewIfNeeded();
    await shell.click();
    const pm = page.getByTestId("reply-text-surface").locator(".ProseMirror").first();
    await expect(pm).toBeVisible();
    await pm.pressSequentially("first reply body", { delay: 5 });

    const sendBtn = page.getByTestId("reply-text-surface").locator("button:has(svg.icon-arrow-up)").first();
    await expect(sendBtn).toBeEnabled();
    await sendBtn.click();

    // Locked: the editor host is pointer-events-none + dimmed.
    const host = pm.locator('xpath=ancestor::div[contains(@class,"overflow-y-auto")][1]');
    await expect(host).toHaveClass(/pointer-events-none/);

    // Press the dimmed editor's first line: it falls through to the column,
    // and the text surface must not take it over — focus leaves the editor.
    const pmBox = await pm.boundingBox();
    if (!pmBox) throw new Error("reply editor has no bounding box");
    await page.mouse.click(pmBox.x + 10, pmBox.y + 8);
    expect(
      await page.evaluate(() => !!document.activeElement?.closest?.(".ProseMirror")),
    ).toBe(false);
    await page.keyboard.type("ZZ");
    await expect(pm).toHaveText("first reply body");

    // The surface's own padding band also stands down while the lock is on.
    const surfaceBox = await page.getByTestId("reply-text-surface").boundingBox();
    if (!surfaceBox) throw new Error("reply text surface has no bounding box");
    await page.mouse.click(surfaceBox.x + surfaceBox.width / 2, surfaceBox.y + 3);
    expect(
      await page.evaluate(() => !!document.activeElement?.closest?.(".ProseMirror")),
    ).toBe(false);
    await expect(pm).toHaveText("first reply body");

    // Once the server accepts, the box clears (thread replies stay mounted).
    await expect(pm).toHaveText("", { timeout: 15000 });
  });
});
