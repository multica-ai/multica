import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault, waitForPageText } from "./helpers";
import type { TestApiClient } from "./fixtures";

// MUL-8100: absolutely positioned content inside the issue's right sidebar
// (the execution log's `sr-only` status labels) must stay clipped by the
// sidebar's own scroller. When it resolved against an ancestor outside the
// scroller, it stretched the surrounding panel's scroll height; wheeling past
// the end of the comments then scrolled the whole issue detail up, leaving a
// blank band at the bottom.
test.describe("Issue detail sidebar containment", () => {
  let api: TestApiClient;

  test.beforeEach(async () => {
    api = await createTestApi();
  });

  test.afterEach(async () => {
    await api?.cleanup();
  });

  test("absolutely positioned sidebar content does not grow the outer panels", async ({ page }) => {
    // Short window so the sidebar overflows, like a long execution log does.
    await page.setViewportSize({ width: 1440, height: 520 });
    const title = "E2E sidebar containment " + Date.now();
    const issue = await api.createIssue(title);
    const slug = await loginAsDefault(page);
    await page.goto(`/${slug}/issues/${issue.id}`, { waitUntil: "domcontentloaded" });
    await waitForPageText(page, title);
    await expect(page.locator("text=Properties")).toBeVisible();

    const result = await page.evaluate(() => {
      const sidebar = document.querySelector<HTMLElement>("[data-right-sidebar-panel] > div > div")!;
      const content = sidebar.firstElementChild as HTMLElement;
      // Mirror an execution-log row far below the fold: an `sr-only` label
      // (position: absolute) inside non-positioned flex rows.
      const row = document.createElement("div");
      row.className = "flex overflow-hidden";
      row.style.marginTop = "1500px";
      const label = document.createElement("span");
      label.className = "sr-only";
      label.textContent = "Completed";
      row.appendChild(label);
      content.appendChild(row);

      // Every scroller that wraps the whole detail (resizable panels, the
      // page canvas) must stay exactly as tall as its box.
      const grown: string[] = [];
      for (let el = sidebar.parentElement; el; el = el.parentElement) {
        const overflowY = getComputedStyle(el).overflowY;
        if (overflowY === "visible") continue;
        if (el.scrollHeight > el.clientHeight + 1) {
          grown.push(`${el.tagName}.${el.className} ${el.scrollHeight}>${el.clientHeight}`);
        }
      }
      return { containedBySidebar: label.offsetParent === sidebar, grown };
    });

    expect(result.containedBySidebar).toBe(true);
    expect(result.grown).toEqual([]);
  });
});
