import { test, expect, type FileChooser } from "@playwright/test";
import { TestApiClient } from "./fixtures";

let api: TestApiClient;
let slug: string;
test.beforeEach(async ({ page }) => {
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
  await api.cleanup();
  await api.deleteWorkspace();
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
  await page.getByRole("link", { name: "History", exact: true }).click();
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
  await expect(editor).toBeVisible();
  await page.clock.install({ time: new Date("2026-11-09T12:00:00Z") });
  await page.clock.pauseAt(new Date("2026-11-09T12:00:01Z"));

  const saving = page.waitForRequest((request) =>
    request.method() === "PATCH" && request.url().endsWith(`/api/docs/${doc.id}`),
  );
  await title.fill("Final title");
  await editor.fill("Last Markdown edit before leaving.");
  await page.getByRole("link", { name: "History", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/${doc.id}/history$`));
  expect((await saving).postDataJSON()).toMatchObject({
    title: "Final title", content: "Last Markdown edit before leaving.", base_revision: 1,
  });
  await expect.poll(async () => api.getDoc(doc.id)).toMatchObject({
    title: "Final title", content: "Last Markdown edit before leaving.", revision: 2, version_count: 2,
  });
});

test("stale save keeps a draft across reload and lets the human load the latest", async ({
  page,
}, info) => {
  const doc = await api.createDoc("Design notes", "Original shared content");
  await page.goto(`/${slug}/docs/${doc.id}`);
  const title = page.getByRole("textbox", { name: "Title", exact: true });
  await expect(title).toHaveValue("Design notes");
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
  const editor = page.locator('[contenteditable="true"]').first();
  await editor.fill("A shared architecture decision.");
  await expect.poll(async () => (await api.getDoc(id)).content).toContain("A shared architecture decision.");
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
  const content = async () => (await api.getDoc(doc.id)).content;
  await expect(page.getByRole("status").filter({ hasText: "Saved" })).toBeVisible();
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
