import { test, expect, type FileChooser, type Page } from "@playwright/test";
// Docs sits behind the docs_v1 flag: run the server with FF_DOCS_V1=true.
//
// The spec passes with or without collab, and each covers a different save path:
// - `make up` (API + collab + Web): docs edit live, the header says "Live",
//   and collab stores the body. Reads pass `flush` so they see what was typed
//   without waiting out collab's store debounce.
// - `make up C=api,web` after `make down C=collab`: docs save through the API
//   and the header says "Saved". A running collab stays wired to the API
//   whatever `C=` says, so stop it first.
// Tests read the mode from that status. The few that exist in one mode only
// skip in the other and say why; run both setups to cover everything.
import { TestApiClient } from "./fixtures";

type DocMode = "live" | "solo";

/** Waits for the open doc to settle into a mode. The wait outlasts the page's 8s fallback to solo. */
async function docMode(page: Page): Promise<DocMode> {
  const status = page.getByRole("status").filter({ hasText: /^(Live|Saved)$/ });
  await expect(status).toBeVisible({ timeout: 10_000 });
  return (await status.textContent())?.trim() === "Live" ? "live" : "solo";
}

let api: TestApiClient;
let slug: string;
const API_BASE = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
let docsEnabledPromise: Promise<boolean> | null = null;
function isDocsEnabled() {
  return (docsEnabledPromise ??= fetch(`${API_BASE}/api/config`)
    .then((res) => (res.ok ? res.json() : null))
    .then((body: { feature_flags?: { docs_v1?: boolean } } | null) => body?.feature_flags?.docs_v1 === true)
    .catch(() => false));
}

test.beforeEach(async ({ page }) => {
  test.skip(!(await isDocsEnabled()), "Docs is off; start the API with FF_DOCS_V1=true.");
  page.on("pageerror", (error) => {
    throw error;
  });
  api = new TestApiClient();
  const suffix = `${Date.now()}-${test.info().workerIndex}`;
  await api.login(`docs-${suffix}@multica.ai`, "Alex Morgan");
  const ws = await api.ensureWorkspace("Product team", `docs-${suffix}`);
  slug = ws.slug;
  await api.markUserOnboarded();
  await page.addInitScript((token) => {
    localStorage.setItem("multica_token", token!);
    localStorage.setItem("multica:chat:isOpen", "false");
  }, api.getToken());
  await page.setViewportSize({ width: 1440, height: 1000 });
});
test.afterEach(async () => {
  await api?.cleanup();
  await api?.deleteWorkspace();
});

test("Markdown autosave, folder move, immutable history and restore", async ({
  page,
}, info) => {
  const folder = await api.createDocFolder("Engineering");
  const original =
    "# Release checklist\n\nBefore each release, the owner verifies the following steps.\n\n## Before release\n\n- Run the tests\n- Review the migration plan\n- Confirm the rollback owner\n\n## After release\n\nCheck the health dashboard and write down the outcome.";
  const doc = await api.createDoc("Release runbook", original);
  await page.goto(`/${slug}/docs/${doc.id}`);
  const title = page.getByRole("textbox", { name: "Title", exact: true });
  await expect(title).toHaveValue("Release runbook");
  await title.fill("Release runbook — updated");
  await expect
    .poll(async () => (await api.getDoc(doc.id)).title)
    .toBe("Release runbook — updated");
  await page.getByRole("button", { name: "More", exact: true }).click();
  await page.getByRole("menuitem", { name: "Move to" }).click();
  await page.getByRole("option", { name: "Engineering", exact: true }).click();
  await page.getByRole("button", { name: "Move", exact: true }).click();
  await expect
    .poll(async () => (await api.getDoc(doc.id)).folder_id)
    .toBe(folder.id);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.screenshot({
    path: info.outputPath("docs-editor.png"),
    animations: "disabled",
    style: "nextjs-portal { display: none; }",
  });
  await page.getByRole("button", { name: "More", exact: true }).click();
  await page.getByRole("menuitem", { name: "History", exact: true }).click();
  await page.getByRole("button", { name: /^v1 / }).click();
  await expect(
    page.getByRole("heading", { name: "Release runbook", exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: info.outputPath("docs-history.png"),
    animations: "disabled",
    style: "nextjs-portal { display: none; }",
  });
  await page.getByRole("button", { name: "Restore this version" }).click();
  await expect(title).toHaveValue("Release runbook");
  const restored = await api.getDoc(doc.id);
  expect(restored.content).toBe(original);
  expect(restored.version_count).toBe(3);
  await page.getByRole("button", { name: "More", exact: true }).click();
  await page.getByRole("menuitem", { name: "Delete", exact: true }).click();
  await expect(page.getByRole("alertdialog")).toBeVisible();
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  expect((await api.getDoc(doc.id)).title).toBe("Release runbook");
});

test("navigating to history saves the last title and Markdown before the debounce", async ({ page }) => {
  const doc = await api.createDoc("Quick edit", "Original");
  // Warm the destination so this checks leaving the editor, not route compilation.
  await page.goto(`/${slug}/docs/${doc.id}/history`);
  await expect(page.getByRole("button", { name: /^v1/ })).toBeVisible();
  await page.goto(`/${slug}/docs/${doc.id}`);
  const title = page.getByRole("textbox", { name: "Title", exact: true });
  const editor = page.locator('[contenteditable="true"]').first();
  await expect(title).toHaveValue("Quick edit");
  const mode = await docMode(page);
  await expect(editor).toBeVisible();
  await page.clock.install({ time: new Date("2026-11-09T12:00:00Z") });
  await page.clock.pauseAt(new Date("2026-11-09T12:00:01Z"));

  const saving = page.waitForRequest((request) =>
    request.method() === "PATCH" && request.url().endsWith(`/api/docs/${doc.id}`),
  );
  await title.fill("Final title");
  await editor.fill("Last Markdown edit before leaving.");
  await page.getByRole("link", { name: /^Edited / }).click();
  await expect(page).toHaveURL(new RegExp(`/${doc.id}/history$`));
  const sent = (await saving).postDataJSON();
  if (mode === "solo") {
    expect(sent).toMatchObject({
      title: "Final title", content: "Last Markdown edit before leaving.", base_revision: 1,
    });
    await expect.poll(async () => api.getDoc(doc.id)).toMatchObject({
      title: "Final title", content: "Last Markdown edit before leaving.", revision: 2, version_count: 2,
    });
  } else {
    // Live, the request carries the title alone; the body went to collab as it was typed.
    expect(sent).toMatchObject({ title: "Final title" });
    expect(sent).not.toHaveProperty("content");
    await expect.poll(async () => api.getDoc(doc.id, { flush: true })).toMatchObject({
      title: "Final title", content: "Last Markdown edit before leaving.",
    });
  }
});

test("stale save keeps a draft across reload and lets the human load the latest", async ({
  page,
}, info) => {
  const doc = await api.createDoc("Design notes", "Original shared content");
  await page.goto(`/${slug}/docs/${doc.id}`);
  const title = page.getByRole("textbox", { name: "Title", exact: true });
  await expect(title).toHaveValue("Design notes");
  test.skip(
    (await docMode(page)) === "live",
    "Live editing merges a colleague's write instead of rejecting a stale save; the next test covers that.",
  );
  await title.fill("My unsaved title");
  await api.updateDoc(doc.id, 1, "A colleague's newer content");
  await expect(
    page.getByRole("alert").filter({ hasText: "Your draft is kept" }),
  ).toBeVisible();
  await page.reload();
  await expect(title).toHaveValue("My unsaved title");
  await expect(
    page.getByRole("alert").filter({ hasText: "Your draft is kept" }),
  ).toBeVisible();
  await page.screenshot({
    path: info.outputPath("docs-conflict.png"),
    animations: "disabled",
    style: "nextjs-portal { display: none; }",
  });
  await page.getByRole("button", { name: "Reload latest" }).click();
  await expect(title).toHaveValue("Design notes");
  await page.getByText("Your saved draft", { exact: true }).click();
  await expect(
    page.getByRole("textbox", { name: "Your saved draft" }),
  ).toHaveValue(/My unsaved title/);
  expect((await api.getDoc(doc.id)).content).toBe(
    "A colleague's newer content",
  );
});

test("a colleague's write merges into a doc open for live editing", async ({ page }) => {
  const doc = await api.createDoc("Design notes", "Original shared content");
  await page.goto(`/${slug}/docs/${doc.id}`);
  const title = page.getByRole("textbox", { name: "Title", exact: true });
  await expect(title).toHaveValue("Design notes");
  test.skip(
    (await docMode(page)) === "solo",
    "Without collab a colleague's write makes the open page's save stale; the previous test covers that.",
  );
  await title.fill("My new title");
  await expect.poll(async () => (await api.getDoc(doc.id)).title).toBe("My new title");
  await api.updateDoc(doc.id, 1, "A colleague's newer content");
  const editor = page.locator('.ProseMirror[contenteditable="true"]').first();
  await expect(editor).toHaveText("A colleague's newer content");
  await expect(title).toHaveValue("My new title");
  await expect(page.getByRole("alert").filter({ hasText: "Your draft is kept" })).toHaveCount(0);
  await expect.poll(async () => api.getDoc(doc.id, { flush: true })).toMatchObject({
    title: "My new title", content: "A colleague's newer content",
  });
});

test("create a nested doc, edit Markdown, and delete it with confirmation", async ({page}) => {
  await page.goto(`/${slug}/docs`);
  await page.getByRole("button", {name: "New", exact: true}).click();
  await page.getByRole("menuitem", {name: "New folder", exact: true}).click();
  await page.getByRole("dialog").getByRole("textbox", {name: "Title", exact: true}).fill("Product");
  await page.getByRole("dialog").getByRole("button", {name: "New", exact: true}).click();
  await page.getByRole("button", {name: "More actions for Product", exact: true}).click();
  await page.getByRole("menuitem", {name: "New doc", exact: true}).click();
  await expect(page).toHaveURL(/\/docs\/[0-9a-f-]{36}$/);
  await expect(page.getByRole("textbox", {name: "Title", exact: true})).toBeFocused();
  await page.keyboard.type("Architecture notes");
  const id = page.url().split("/").at(-1)!;
  await docMode(page);
  const editor = page.locator('[contenteditable="true"]').first();
  await editor.fill("A shared architecture decision.");
  await expect.poll(async () => (await api.getDoc(id, { flush: true })).content).toContain("A shared architecture decision.");
  expect((await api.getDoc(id)).folder_id).not.toBeNull();
  await page.getByRole("button", {name: "More", exact: true}).click();
  await page.getByRole("menuitem", {name: "Delete", exact: true}).click();
  await page.getByRole("alertdialog").getByRole("button", {name: "Delete", exact: true}).click();
  await expect(page).toHaveURL(new RegExp(`/${slug}/docs$`));
  await expect(api.getDoc(id)).rejects.toThrow(/404/);
  await page.getByRole("button", {name: "More actions for Product", exact: true}).click();
  await page.getByRole("menuitem", {name: "Delete", exact: true}).click();
  await page.getByRole("alertdialog").getByRole("button", {name: "Delete", exact: true}).click();
  await expect(page.getByRole("button", {name: "More actions for Product", exact: true})).toHaveCount(0);
});

test("block menu, block handle and outline shape a document", async ({ page }, info) => {
  const doc = await api.createDoc(
    "Launch plan",
    "# Goals\n\nShip Docs.\n\n## Scope\n\n- Editor\n- Outline\n\n## Risks\n\nNone yet.",
  );
  // Registered up front: the file-chooser hook must be armed before the key
  // press that opens the picker, or the dialog slips past it.
  const choosers: FileChooser[] = [];
  page.on("filechooser", (chooser) => choosers.push(chooser));
  await page.goto(`/${slug}/docs/${doc.id}`);
  const content = async () => (await api.getDoc(doc.id, { flush: true })).content;
  await docMode(page);
  await expect(page.locator("time")).toHaveText(/^Edited /);

  const outline = page.getByRole("navigation", { name: "Outline" });
  await expect(outline.getByRole("button")).toHaveText(["Goals", "Scope", "Risks"]);

  // `/` opens the block menu; Enter inserts the highlighted block.
  // The chat composer is the second editor on the page.
  const editor = page.locator('.ProseMirror[contenteditable="true"]').first();
  // An instant synthetic click can land before ProseMirror reads the DOM
  // selection; retry until the editor's own selection is in the paragraph.
  await expect(async () => {
    await page.getByText("None yet.", { exact: true }).click();
    expect(
      await editor.evaluate(
        (dom) =>
          (dom as unknown as { editor: { state: { selection: { $from: { parent: { textContent: string } } } } } })
            .editor.state.selection.$from.parent.textContent,
      ),
    ).toBe("None yet.");
  }).toPass();
  await page.keyboard.press("End");
  await page.keyboard.press("Enter");
  await page.keyboard.type("/h2");
  await expect(page.getByRole("listbox", { name: "Insert block" })).toBeVisible();
  // Pausing past the autosave must keep the caret and the open menu.
  await expect.poll(content).toContain("/h2");
  await expect(page.getByRole("listbox", { name: "Insert block" })).toBeVisible();
  await page.keyboard.press("Enter");
  await page.keyboard.type("Timeline");
  await expect.poll(content).toContain("## Timeline");
  await expect(outline.getByRole("button")).toHaveText(["Goals", "Scope", "Risks", "Timeline"]);

  // The handle's grip opens turn-into. Glide to it like a person: the path
  // crosses the margin between the text and the handle.
  const line = (await page.getByText("Ship Docs.", { exact: true }).boundingBox())!;
  await page.mouse.move(line.x + line.width / 2, line.y + line.height - 2);
  const grip = page.getByRole("button", { name: "Drag to move, click for options" });
  const gripBox = (await grip.boundingBox())!;
  await page.mouse.move(gripBox.x + gripBox.width / 2, gripBox.y + gripBox.height / 2, {
    steps: 40,
  });
  await page.waitForTimeout(400);
  await expect(grip).toBeVisible();
  await page.mouse.down();
  await page.mouse.up();
  await expect(page.getByRole("group", { name: "Turn into" })).toBeVisible();
  await page.getByRole("menuitem", { name: "Quote", exact: true }).click();
  await expect.poll(content).toContain("> Ship Docs.");

  // Dragging the grip moves the block.
  await page.getByText("None yet.", { exact: true }).hover();
  await page
    .getByRole("button", { name: "Drag to move, click for options" })
    .dragTo(editor.getByRole("heading", { name: "Goals" }), {
      targetPosition: { x: 4, y: 2 },
    });
  await expect.poll(content).toMatch(/^None yet\.\n\n# Goals/);

  // "+" opens the menu on a new line; Image uploads through the file picker.
  await page.getByRole("heading", { name: "Timeline" }).hover();
  await page.getByRole("button", { name: "Add below" }).click();
  await expect(page.getByRole("listbox", { name: "Insert block" })).toBeVisible();
  await page.keyboard.type("image");
  await page.keyboard.press("Enter");
  await expect.poll(() => choosers.length).toBe(1);
  await choosers[0]!.setFiles({
    name: "diagram.png",
    mimeType: "image/png",
    buffer: Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
      "base64",
    ),
  });
  await expect.poll(content).toMatch(/!\[diagram\.png\]\([^)]*\/api\/attachments\/[0-9a-f-]{36}\/download\)/);

  await page.mouse.move(0, 0);
  await page.screenshot({
    path: info.outputPath("docs-blocks.png"),
    animations: "disabled",
    style: "nextjs-portal { display: none; }",
  });
});

test("docs link from issues, show up in Cmd+K, and reorganize from the tree", async ({
  page,
}, info) => {
  const folder = await api.createDocFolder("Engineering");
  const doc = await api.createDoc("Release runbook", "Steps for every release.");
  const issue = await api.createIssue("Ship the release", {
    description: `See [Release runbook](mention://doc/${doc.id}).`,
  });

  // A doc mention renders as a chip that opens the doc.
  await page.goto(`/${slug}/issues/${issue.id}`);
  const chip = page.locator("a.doc-mention").filter({ hasText: "Release runbook" }).first();
  await expect(chip).toHaveAttribute("href", new RegExp(`/${slug}/docs/${doc.id}$`));

  // `@` offers docs in their own section.
  await expect(page.locator("text=Properties")).toBeVisible();
  const composer = page
    .locator('.ProseMirror[data-placeholder="Leave a comment..."], .ProseMirror:has([data-placeholder="Leave a comment..."])')
    .first();
  await expect(async () => {
    await page.getByTestId("comment-composer-shell").click({ timeout: 2000 });
    await composer.click({ force: true, timeout: 2000 });
  }).toPass();
  await page.keyboard.type("@runbook");
  await page.getByRole("button", { name: "Release runbook" }).click();
  await page.keyboard.press("ControlOrMeta+Enter");
  await expect(page.locator("a.doc-mention")).toHaveCount(2);
  await page.screenshot({ path: info.outputPath("docs-mention.png"), animations: "disabled", style: "nextjs-portal { display: none; }" });

  // Cmd+K finds the doc by title.
  await page.getByRole("button", { name: "Search..." }).click();
  await page.getByRole("combobox", { name: "Command bar" }).fill("runbook");
  await page.getByRole("group", { name: "Docs" }).getByRole("option", { name: /Release runbook/ }).click();
  await expect(page).toHaveURL(new RegExp(`/docs/${doc.id}$`));

  // Drag the doc onto the folder in the tree.
  await page
    .getByRole("link", { name: "Release runbook", exact: true })
    .dragTo(page.getByText("Engineering", { exact: true }));
  await expect.poll(async () => (await api.getDoc(doc.id)).folder_id).toBe(folder.id);

  // The folder's "+" opens a new untitled doc inside it, caret in the title.
  await page.getByRole("complementary").getByText("Engineering", { exact: true }).hover();
  await page.getByRole("button", { name: "New doc in Engineering" }).click();
  await expect(page).not.toHaveURL(new RegExp(doc.id));
  await expect(page).toHaveURL(/\/docs\/[0-9a-f-]{36}$/);
  const created = page.url().split("/").at(-1)!;
  const title = page.getByRole("textbox", { name: "Title", exact: true });
  await expect(title).toBeFocused();
  await page.keyboard.type("On-call handbook");
  await expect.poll(async () => (await api.getDoc(created)).title).toBe("On-call handbook");
  expect((await api.getDoc(created)).folder_id).toBe(folder.id);
  await page.screenshot({ path: info.outputPath("docs-tree.png"), animations: "disabled", style: "nextjs-portal { display: none; }" });
});

test("a doc or project mention's link is no wider than its truncated chip", async ({ page }) => {
  // A flex link around the chip drops the chip's min(18rem, 100%) cap while
  // sizing itself, so a long title leaves a blank gap after the truncated
  // chip and pushes the following text away (MUL-9859).
  const doc = await api.createDoc(
    "GEO baseline report: share of voice across buyer prompts and competitor citations",
    "Baseline.",
  );
  const project = await api.createProject(
    "GEO observability: daily buyer prompt tracking and citation share dashboard",
  );
  const body = [
    "1. Fix existing pages first",
    `   - The [baseline](mention://doc/${doc.id}) confirms it, and [the project](mention://project/${project.id}) tracks it.`,
    "",
    `In a paragraph: [baseline](mention://doc/${doc.id}) and [the project](mention://project/${project.id}).`,
  ].join("\n");
  const issue = await api.createIssue("Mention chips in a list", { description: body });
  await api.createComment(issue.id, body);

  // The description renders through the editor, the comment through the
  // readonly renderer: two of each chip on both.
  await page.goto(`/${slug}/issues/${issue.id}`);
  await expect(page.locator(".doc-chip").filter({ hasText: "GEO baseline report" })).toHaveCount(4);
  await expect(page.locator(".project-chip").filter({ hasText: "GEO observability" })).toHaveCount(4);

  const gaps = await page.locator("a.doc-mention, a.project-mention").evaluateAll((links) =>
    links.map((link) => {
      const chip = link.querySelector(".doc-chip, .project-chip")!;
      const { marginLeft, marginRight } = getComputedStyle(chip);
      const hug = chip.getBoundingClientRect().width + parseFloat(marginLeft) + parseFloat(marginRight);
      return Math.round(Math.abs(link.getBoundingClientRect().width - hug));
    }),
  );
  expect(gaps).toEqual(Array(8).fill(0));
});

test("Cmd+K finds a doc by words that only appear in its content", async ({ page }) => {
  const doc = await api.createDoc(
    "Release runbook",
    "# Rollout\n\nBefore cutover, rotate the zephyrine credentials and confirm the rollback owner.",
  );
  await page.goto(`/${slug}/docs`);
  await page.getByRole("button", { name: "Search..." }).click();
  await page.getByRole("combobox", { name: "Command bar" }).fill("zephyrine credentials");
  const hit = page
    .getByRole("group", { name: "Docs" })
    .getByRole("option", { name: /Release runbook/ });
  await expect(hit).toContainText("rotate the zephyrine credentials");
  await hit.click();
  await expect(page).toHaveURL(new RegExp(`/docs/${doc.id}$`));
});

test("comment on selected text, reply, find it again after reload, resolve and reopen", async ({ page }, info) => {
  const doc = await api.createDoc(
    "Release runbook",
    "# Before release\n\nMigrations must rehearse a day ahead in staging.\n\nConfirm the rollback owner.",
  );
  await page.goto(`/${slug}/docs/${doc.id}`);
  const editor = page.locator('.ProseMirror[contenteditable="true"]').first();
  await expect(editor).toBeVisible();

  // Select a word, then "Comment" in the selection toolbar.
  await editor.getByText("Migrations must rehearse", { exact: false }).dblclick({ position: { x: 150, y: 8 } });
  const selected = await page.evaluate(() => window.getSelection()?.toString().trim());
  expect(selected).toBeTruthy();
  await page.getByRole("button", { name: "Comment", exact: true }).click();
  const panel = page.getByRole("complementary", { name: "Comments" });
  await expect(panel).toBeVisible();
  // The new discussion's composer takes focus once it has mounted.
  await expect(panel.locator('.ProseMirror[contenteditable="true"]').first()).toBeFocused();
  await page.keyboard.type("Can index-only migrations rehearse the same day?");
  await panel.getByRole("button", { name: "Comment", exact: true }).click();

  const thread = panel.getByRole("group", { name: selected! });
  await expect(thread).toContainText("Can index-only migrations rehearse the same day?");
  await expect(page.getByRole("button", { name: /^Comments\s*1$/ })).toBeVisible();
  await expect.poll(() => page.evaluate(() => CSS.highlights.has("doc-comment") || CSS.highlights.has("doc-comment-active"))).toBe(true);

  await thread.click();
  await panel.getByRole("button", { name: "Reply", exact: true }).waitFor();
  const replyBox = thread.locator('.ProseMirror[contenteditable="true"]');
  await replyBox.click();
  await expect(replyBox).toBeFocused();
  await page.keyboard.type("Yes, with CONCURRENTLY.");
  await panel.getByRole("button", { name: "Reply", exact: true }).click();
  // Sent: the composer clears and the reply renders as a message.
  await expect(replyBox).toHaveText("");
  await expect(thread.getByText("Yes, with CONCURRENTLY.", { exact: true })).toBeVisible();
  await page.screenshot({ path: info.outputPath("doc-comments.png"), animations: "disabled", style: "nextjs-portal { display: none; }" });

  // After a reload the highlight is found again by text; clicking it opens the discussion.
  await page.reload();
  await expect(page.getByRole("button", { name: /^Comments\s*1$/ })).toBeVisible();
  await expect.poll(() => page.evaluate(() => CSS.highlights.get("doc-comment")?.size ?? 0)).toBe(1);
  await editor.getByText("Migrations must rehearse", { exact: false }).click({ position: { x: 150, y: 8 } });
  await expect(panel.getByRole("group", { name: selected! }).getByText("Yes, with CONCURRENTLY.", { exact: true })).toBeVisible();

  // Clicking into the open discussion's reply box leaves the doc where it is.
  await panel.getByRole("group", { name: selected! }).locator('.ProseMirror[contenteditable="true"]').click();
  await expect(panel.getByRole("group", { name: selected! }).locator('.ProseMirror[contenteditable="true"]')).toBeFocused();

  // Resolving keeps the open list in view and clears the highlight.
  await panel.getByRole("button", { name: "Resolve", exact: true }).click();
  await expect(panel.getByRole("tab", { name: "Open 0" })).toHaveAttribute("aria-selected", "true");
  await expect(panel.getByRole("tab", { name: "Resolved 1" })).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => (CSS.highlights.get("doc-comment")?.size ?? 0) + (CSS.highlights.get("doc-comment-active")?.size ?? 0)))
    .toBe(0);

  // Reopening brings it back to the open list.
  await panel.getByRole("tab", { name: "Resolved 1" }).click();
  await panel.getByRole("button", { name: "Reopen", exact: true }).click();
  await expect(panel.getByRole("tab", { name: "Open 1" })).toHaveAttribute("aria-selected", "true");
  await expect(panel.getByRole("group", { name: selected! })).toBeVisible();
});
