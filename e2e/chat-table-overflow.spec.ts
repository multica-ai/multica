import { readFileSync } from "node:fs";
import { expect, test } from "@playwright/test";

// CSS geometry regression: no API/server needed. This fixture mirrors the
// RichContent table > IssueMentionCard > IssueChip DOM. The existing component
// tests cover issue resolution/navigation; a real browser owns intrinsic sizing.
const prose = readFileSync("packages/views/editor/styles/prose.css", "utf8");
const richContent = readFileSync("packages/views/rich-content/rich-content.css", "utf8");
const title = "A very long issue title with an ExtremelyLongUnbrokenIdentifier1234567890";
const chip = (id: number) => `<span><a class="issue-mention" href="#PX-${id}"><span
  class="issue-mention chip"><svg width="14" height="14" aria-hidden="true"><circle cx="7" cy="7" r="5" fill="none" stroke="currentColor" /></svg>
  <span class="identifier">PX-${id}</span><span class="issue-mention-title truncate">${title}</span>
</span></a></span>`;

// Only the chip's utility styles are supplied by the fixture; the table and
// compact-reference rules under test come directly from production stylesheets.
const utilities = `
  * { box-sizing: border-box; }
  body { font: 14px Arial; margin: 16px; --text-body: 14px; --border: #ddd; --radius: 6px; }
  a { color: inherit; text-decoration: none; }
  .chip { display: inline-flex; min-width: 0; max-width: min(18rem,100%);
    align-items: center; gap: 6px; border: 1px solid #ddd; margin: 0 2px;
    padding: 2px 8px; font-size: 12px; border-radius: 6px; }
  .identifier, svg { flex-shrink: 0; }
  .truncate { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
`;

for (const width of [360, 640, 960]) {
  test(`all columns remain reachable throughout a tall ${width}px report`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width: width + 32, height: 800 });
    const rows = Array.from({ length: 20 }, (_, row) => `<tr><td>Agent ${row + 1}</td>${
      Array.from({ length: 3 }, (_, column) => `<td>${
        [0, 1, 2].map((n) => chip(613 + column * 3 + n)).join(" ")
      }</td>`).join("")
    }</tr>`).join("");
    await page.setContent(`<style>${utilities}${prose}</style>
      <main style="width:${width}px" data-rich-content class="rich-text-editor rich-content-compact">
        <p id="prose">Follow up on ${chip(613)}.</p>
        <div class="tableWrapper"><table><thead><tr><th>Agent</th><th>Done</th><th>In progress</th><th>Backlog</th></tr></thead><tbody>${rows}</tbody></table></div>
        <pre><code>PX-613 is literal code</code></pre>
      </main>`);
    const proseBefore = await page.locator("#prose .chip").boundingBox();
    if (width === 640) {
      await testInfo.attach("before", { body: await page.screenshot(), contentType: "image/png" });
    }
    await page.addStyleTag({ content: richContent });
    const wrapper = page.locator(".tableWrapper");
    expect(await wrapper.evaluate((el) => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(1);
    expect(await page.locator("#prose .chip").boundingBox()).toEqual(proseBefore);
    await expect(page.locator("#prose .issue-mention-title")).toBeVisible();
    await expect(page.locator("code")).toHaveText("PX-613 is literal code");
    await expect(page.locator("tbody .issue-mention-title").first()).toBeHidden();
    for (const row of [0, 10, 19]) {
      const lastCell = page.locator("tbody tr").nth(row).locator("td").last();
      await lastCell.scrollIntoViewIfNeeded();
      const cell = await lastCell.boundingBox();
      expect(cell!.x + cell!.width).toBeLessThanOrEqual(width + 17);
      await expect(lastCell.locator("a")).toHaveCount(3);
      await expect(lastCell.locator(".identifier").first()).toHaveText("PX-619");
      await lastCell.locator("a").first().focus();
      await page.keyboard.press("Enter");
      await expect(page).toHaveURL(/#PX-619$/);
    }
    await page.evaluate(() => window.scrollTo(0, 0));
    await testInfo.attach("after", { body: await page.screenshot(), contentType: "image/png" });
  });
}
