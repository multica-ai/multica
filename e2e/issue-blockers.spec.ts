import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";

test("waiting tasks can be added, filtered and removed without changing status", async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  const api = await createTestApi();
  try {
    const slug = await loginAsDefault(page);
    const blocker = await api.createIssue("Prepare the API contract", { status: "in_progress" });
    const downstream = await api.createIssue("Build the project dashboard", { status: "todo" });
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(`/${slug}/issues/${downstream.identifier}`, {waitUntil:"domcontentloaded"});
    await page.getByRole("button", { name: "Add blocker", exact: true }).click();
    const picker = page.getByRole("dialog");
    await picker.getByPlaceholder("Search issues...").fill(blocker.title);
    await picker.getByText(blocker.title).click();
    await expect(picker).not.toBeVisible();
    await expect(page.getByRole("link", { name: `${blocker.identifier} ${blocker.title}`, exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Todo", exact: true }).first()).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("waiting-detail.png"), fullPage: true });
    await page.goto(`/${slug}/issues`, {waitUntil:"domcontentloaded"});
    await expect(page.getByLabel("Waiting for 1 task").first()).toBeVisible();
    await page.getByRole("button", {name:"Filter",exact:true}).click();
    await page.getByRole("menuitem", {name:/^Relationships/}).click();
    await page.getByRole("menuitemcheckbox", {name:"Blocked",exact:true}).click();
    await page.keyboard.press("Escape");
    await page.keyboard.press("Escape");
    await expect(page.getByText(downstream.title,{exact:true}).first()).toBeVisible();
    await expect(page.getByText(blocker.title,{exact:true})).not.toBeVisible();
    await page.mouse.click(1080, 300);
    await expect(page.getByRole("menu")).toHaveCount(0);
    await page.screenshot({ path: testInfo.outputPath("waiting-list.png"), fullPage: true, animations: "disabled" });
    await page.goto(`/${slug}/issues/${downstream.identifier}`, {waitUntil:"domcontentloaded"});
    await page.getByRole("button", { name: `Remove ${blocker.identifier}`, exact: true }).click();
    await expect(page.getByRole("link", { name: `${blocker.identifier} ${blocker.title}`, exact: true })).not.toBeVisible();
    await expect(page.getByRole("button", { name: "Todo", exact: true }).first()).toBeVisible();
  } finally {
    await api.cleanup();
  }
});
