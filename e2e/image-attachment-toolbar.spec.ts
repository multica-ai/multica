import { expect, test, type Page } from "@playwright/test";
import type { TestApiClient } from "./fixtures";
import { createTestApi, loginAsDefault } from "./helpers";

async function imageMarkdown(page: Page, alt: string, width: number, height: number) {
  const url = `https://fixture-images.invalid/${width}x${height}.svg`;
  await page.route(url, (route) => route.fulfill({
    contentType: "image/svg+xml",
    body: `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}"><rect width="100%" height="100%" fill="royalblue"/></svg>`,
  }));
  return `![${alt}](${url})`;
}

test.describe("image attachment toolbar", () => {
  let api: TestApiClient;
  let workspaceSlug: string;

  test.beforeEach(async ({ page, baseURL }) => {
    const local = (url: string) => ["localhost", "127.0.0.1", "[::1]"].includes(new URL(url).hostname);
    const apiBase = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
    test.skip(!baseURL || !local(baseURL) || !local(apiBase), "Local fixtures only");
    api = await createTestApi();
    workspaceSlug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api?.cleanup();
  });

  test("can open a tall editor image when neither side has room", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 640 });
    const description = await imageMarkdown(page, "Tall editor image", 420, 1800);
    const issue = await api.createIssue("Tall image toolbar", { description });
    await page.goto(`/${workspaceSlug}/issues/${issue.identifier}`);

    const image = page.locator(".ProseMirror").getByAltText("Tall editor image");
    await expect.poll(() => image.evaluate((img: HTMLImageElement) => img.naturalHeight)).toBe(1800);
    await image.evaluate((img) => {
      img.scrollIntoView({ block: "start" });
      const scroller = img.closest<HTMLElement>("[data-tab-scroll-root]")!;
      scroller.scrollTop += img.getBoundingClientRect().top + 16;
    });
    const bounds = (await image.boundingBox())!;
    expect(bounds.y).toBeLessThan(0);
    expect(bounds.y + bounds.height).toBeGreaterThan(640);

    await image.hover({ position: { x: 24, y: 220 } });
    const view = image.locator("..").getByRole("button", { name: "View image" });
    await expect(view).toBeInViewport({ ratio: 1 });
    await view.click();
    await expect(page.getByRole("dialog")).toBeVisible();
  });

  test("keeps the table width when a small image toolbar is hidden", async ({ page }) => {
    const image = await imageMarkdown(page, "Small table image", 24, 18);
    const issue = await api.createIssue("Table image toolbar");
    const comment = await api.createComment(issue.id, `| Text | Image |\n| --- | --- |\n| Content | ${image} |`);
    await page.goto(`/${workspaceSlug}/issues/${issue.identifier}`);

    const tableWrapper = page.locator(`[data-comment-content="${comment.id}"] .tableWrapper`);
    const thumbnail = tableWrapper.getByAltText("Small table image");
    await expect.poll(() => thumbnail.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(24);
    await tableWrapper.evaluate((wrapper) => {
      wrapper.style.width = "340px";
      wrapper.querySelector("table")!.style.tableLayout = "fixed";
      wrapper.querySelector<HTMLElement>("th:last-child")!.style.width = "90px";
    });

    const toolbar = thumbnail.locator("..").locator(".image-toolbar");
    await expect(toolbar).toHaveCSS("opacity", "0");
    await expect.poll(() => tableWrapper.evaluate((wrapper) => wrapper.scrollWidth - wrapper.clientWidth)).toBe(0);
  });
});
