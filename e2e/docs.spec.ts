import { test, expect } from "@playwright/test";
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
  await page.getByRole("dialog").getByRole("textbox", {name: "Title", exact: true}).fill("Architecture notes");
  await page.getByRole("dialog").getByRole("button", {name: "New", exact: true}).click();
  await expect(page.getByRole("textbox", {name: "Title", exact: true})).toHaveValue("Architecture notes");
  await expect(page).toHaveURL(/\/docs\/[0-9a-f-]{36}$/);
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
