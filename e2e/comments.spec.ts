import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Comments", () => {
  let api: TestApiClient;
  let workspaceSlug: string;
  let issueId: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    workspaceSlug = await loginAsDefault(page);
    const issue = await api.createIssue("E2E Comment Test " + Date.now());
    issueId = issue.id;
    await page.goto(`/${workspaceSlug}/issues/${issueId}`);
    await page.waitForURL(new RegExp(`/${workspaceSlug}/issues/${issueId}$`));
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("can add a comment on an issue", async ({ page }) => {
    await expect(page.locator("text=Properties")).toBeVisible();

    const commentText = "E2E comment " + Date.now();
    const commentEditor = page
      .locator(".ProseMirror")
      .filter({
        has: page.locator('p[data-placeholder="Leave a comment..."]'),
      })
      .first();
    await commentEditor.click();
    await page.keyboard.type(commentText);
    await page.evaluate((text) => {
      const editor = Array.from(document.querySelectorAll(".ProseMirror")).find(
        (node) => node.textContent?.includes(text),
      );
      const editorShell = editor?.closest("div.relative.flex.min-h-full.flex-col");
      const container = editorShell?.parentElement?.parentElement;
      const buttons = container
        ? Array.from(container.querySelectorAll("button"))
        : [];
      const submit = buttons[buttons.length - 1];
      if (!(submit instanceof HTMLButtonElement)) {
        throw new Error("comment submit button not found");
      }
      submit.click();
    }, commentText);

    await expect(page.locator(`text=${commentText}`)).toBeVisible({
      timeout: 5000,
    });
  });

  test("comment submit button is disabled when empty", async ({ page }) => {
    await expect(page.locator("text=Properties")).toBeVisible();

    const isDisabled = await page.evaluate(() => {
      const placeholder = document.querySelector(
        'p[data-placeholder="Leave a comment..."]',
      );
      const editorShell = placeholder?.closest("div.relative.flex.min-h-full.flex-col");
      const container = editorShell?.parentElement?.parentElement;
      const buttons = container
        ? Array.from(container.querySelectorAll("button"))
        : [];
      const submit = buttons[buttons.length - 1];
      return submit instanceof HTMLButtonElement ? submit.disabled : null;
    });

    expect(isDisabled).toBe(true);
  });
});
