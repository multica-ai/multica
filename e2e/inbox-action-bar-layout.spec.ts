import { expect, test } from "@playwright/test";
import { TestApiClient } from "./fixtures";

for (const width of [1440, 390]) {
  for (const sticky of [true, false]) {
    test(`Inbox actions leave room for issue content at ${width}px with pinning ${sticky ? "on" : "off"}`, async ({ page }, testInfo) => {
      test.setTimeout(120_000);
      await page.setViewportSize({ width, height: 820 });
      await page.emulateMedia({ reducedMotion: "reduce", colorScheme: "light" });
      const api = new TestApiClient();
      const slug = `inbox-layout-${Date.now()}-${process.pid}`;
      const title = "Inbox action bar layout";
      await api.login(`${slug}@localhost`, "Layout tester");
      await api.ensureWorkspace("Inbox layout test", slug);
      await api.markUserOnboarded();

      try {
        const description = Array.from({ length: 24 }, (_, index) =>
          `Step ${index + 1}. Read the [layout reference ${index + 1}](https://example.com/layout) before continuing with the review.`,
        ).join("\n\n");
        const issue = await api.createIssue(title, { description });
        const first = await api.createComment(issue.id, "Review started.");
        const last = await api.createComment(issue.id, "Review complete. See the [final checklist](https://example.com/checklist).");
        await api.seedCommentNotification(issue.id, first.id, title);
        await page.addInitScript(({ token, sticky }) => {
          localStorage.setItem("multica_token", token!);
          localStorage.setItem("multica:chat:isOpen", "false");
          localStorage.setItem("multica_comment_composer", JSON.stringify({ state: { sticky }, version: 0 }));
        }, { token: api.getToken(), sticky });
        await page.goto(`/${slug}/inbox`);
        await page.getByText(title, { exact: true }).first().click();

        const issueScroll = page.locator(`[data-tab-scroll-root="main:${issue.id}"]`);
        const bar = page.locator('[data-inbox-bar="open"]');
        await expect(bar).toBeVisible();
        const composer = issueScroll.getByTestId("comment-composer-shell");
        await expect(composer).toBeAttached();
        await issueScroll.evaluate((element) => { element.scrollTop = 120; });
        await page.screenshot({
          path: testInfo.outputPath("description.png"),
          animations: "disabled",
          style: "nextjs-portal { display: none !important; }",
        });
        const descriptionLink = issueScroll.getByRole("link", { name: "layout reference 12", exact: true });
        const toolbar = (await bar.boundingBox())!;
        const readingArea = (await issueScroll.boundingBox())!;
        expect(toolbar.y).toBeGreaterThanOrEqual(readingArea.y + readingArea.height);
        await descriptionLink.focus();
        await expect(descriptionLink).toBeInViewport();
        await descriptionLink.click({ trial: true });

        const lastLink = issueScroll.locator(`#comment-${last.id}`).getByRole("link", { name: "final checklist" });
        await issueScroll.evaluate((element) => { element.scrollTop = element.scrollHeight; });
        await expect(lastLink).toBeInViewport();
        await lastLink.click({ trial: true });
        await expect(composer).toBeInViewport();
        await page.screenshot({
          path: testInfo.outputPath("activity-end.png"),
          animations: "disabled",
          style: "nextjs-portal { display: none !important; }",
        });

        const status = bar.getByRole("button", { name: "Todo", exact: true });
        await status.click();
        await expect(status).toHaveAttribute("aria-expanded", "true");
        await page.keyboard.press("Escape");
        const assignee = bar.getByRole("button", { name: "Unassigned", exact: true });
        await assignee.click();
        await expect(assignee).toHaveAttribute("aria-expanded", "true");
        await page.keyboard.press("Escape");

        const snooze = bar.getByRole("button", { name: "Snooze", exact: true });
        await snooze.click();
        await expect(snooze).toHaveAttribute("aria-expanded", "true");
        await page.keyboard.press("Escape");
        await expect(snooze).toHaveAttribute("aria-expanded", "false");
        await lastLink.focus();
        await page.keyboard.press("h");
        await expect(snooze).toHaveAttribute("aria-expanded", "true");
        await page.keyboard.press("Escape");
        await expect(snooze).toHaveAttribute("aria-expanded", "false");
        await lastLink.focus();
        await expect(bar.getByRole("button", { name: "Archive", exact: true })).toBeEnabled();
        await page.keyboard.press("e");
        await expect(page.getByText("Notification archived", { exact: true })).toBeVisible();
        await expect(bar).toHaveCount(0);
      } finally {
        await api.cleanup();
        await api.deleteWorkspace();
      }
    });
  }
}
