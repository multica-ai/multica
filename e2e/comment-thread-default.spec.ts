import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Comment thread starting preference", () => {
  let api: TestApiClient;
  let issueId: string;
  let workspaceSlug: string;
  let rootIds: string[];
  let replyId: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    const issue = await api.createIssue("Thread preference " + Date.now());
    issueId = issue.id;
    rootIds = [];
    for (const number of [1, 2, 3]) {
      const comment = await api.createComment(issueId, `Thread body ${number}`);
      rootIds.push(comment.id);
    }
    replyId = (await api.createComment(issueId, "Later reply to the oldest thread", rootIds[0])).id;
    workspaceSlug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api?.cleanup();
  });

  test("keeps all choices readable at a narrow viewport", async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 800 });
    await page.goto(`/${workspaceSlug}/settings?tab=appearance`);
    const group = page.getByRole("group", { name: "Comment threads" });
    await group.getByRole("button", { name: "All but newest", exact: true }).click();
    const choices = await group.getByRole("button").evaluateAll((buttons) =>
      buttons.map((button) => ({
        width: button.clientWidth,
        contentWidth: button.scrollWidth,
        right: button.getBoundingClientRect().right,
      })),
    );
    for (const choice of choices) {
      expect(choice.contentWidth).toBeLessThanOrEqual(choice.width);
      expect(choice.right).toBeLessThanOrEqual(320);
    }
  });

  test("preserves manual and bulk choices and reveals a linked reply", async ({ page }) => {
    await page.goto(`/${workspaceSlug}/settings?tab=appearance`);
    const group = page.getByRole("group", { name: "Comment threads" });
    await expect(group.getByRole("button", { name: "Expanded", exact: true })).toHaveAttribute("aria-pressed", "true");
    await group.getByRole("button", { name: "Collapsed", exact: true }).click();
    await page.goto(`/${workspaceSlug}/issues/${issueId}`);
    const body = (number: number) => page.locator(`[data-comment-content="${rootIds[number - 1]}"]`);
    await expect(page.getByRole("button", { name: "Expand thread", exact: true })).toHaveCount(3);
    await expect(body(1)).toBeHidden();
    const firstRoot = page.locator(`#comment-${rootIds[0]}`);
    await page.locator("[data-tab-scroll-root]").hover();
    await page.mouse.wheel(0, -1200);
    await firstRoot.getByRole("button", { name: "Expand thread", exact: true }).click();
    await expect(body(1)).toBeVisible();
    await firstRoot.getByRole("button", { name: "Collapse thread", exact: true }).press("Enter");
    await expect(body(1)).toBeHidden();
    await firstRoot.getByRole("button", { name: "Expand thread", exact: true }).press("Enter");
    await expect(body(1)).toBeVisible();
    await page.reload();
    await expect(body(1)).toBeVisible();

    await page.goto(`/${workspaceSlug}/settings?tab=appearance`);
    await group.getByRole("button", { name: "All but newest", exact: true }).click();
    await page.goto(`/${workspaceSlug}/issues/${issueId}`);
    await expect(body(1)).toBeVisible();
    await expect(body(2)).toBeHidden();
    await expect(body(3)).toBeVisible();

    await page.goto(`/${workspaceSlug}/settings?tab=appearance`);
    await group.getByRole("button", { name: "Collapsed", exact: true }).click();
    await page.goto(`/${workspaceSlug}/issues/${issueId}`);
    await expect(body(3)).toBeHidden();
    await page.keyboard.press("ControlOrMeta+k");
    await page.getByRole("combobox").fill("Unfold all comments");
    await page.getByText("Unfold all comments", { exact: true }).click();
    for (const number of [1, 2, 3]) await expect(body(number)).toBeVisible();
    await page.reload();
    for (const number of [1, 2, 3]) await expect(body(number)).toBeVisible();

    await page.keyboard.press("ControlOrMeta+k");
    await page.getByRole("combobox").fill("Fold all comments");
    await page.getByText("Fold all comments", { exact: true }).click();
    for (const number of [1, 2, 3]) await expect(body(number)).toBeHidden();
    await page.goto(`/${workspaceSlug}/issues/${issueId}#comment-${replyId}`);
    await expect(page.getByText("Later reply to the oldest thread", { exact: true })).toBeVisible();
    await expect(body(1)).toBeVisible();
  });
});
