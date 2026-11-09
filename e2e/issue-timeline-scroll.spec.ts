import { test, expect, type Page } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";

async function readingPosition(page: Page, commentId?: string) {
  return page.evaluate((id) => {
    const container = document.querySelector<HTMLElement>('[data-tab-scroll-root^="main:"]')!;
    const c = container.getBoundingClientRect();
    const comment = id
      ? container.querySelector<HTMLElement>(`#comment-${id}`)
      : document.elementFromPoint(c.left + c.width / 2, c.top + c.height / 2)?.closest<HTMLElement>('[id^="comment-"]');
    if (!comment || !container.contains(comment)) return null;
    const e = comment.getBoundingClientRect();
    return {
      id: comment.id.slice("comment-".length),
      offset: e.top - c.top,
      visible: e.bottom > c.top && e.top < c.bottom,
      mounted: comment.dataset.virtualThreadMounted !== "false",
    };
  }, commentId);
}

test("Inbox restores the current reply after leaving a virtualized long thread", async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  await page.setViewportSize({ width: 1440, height: 900 });
  const api = await createTestApi();
  try {
    const slug = await loginAsDefault(page);
    const title = "Inbox long-thread scroll restoration";
    const issue = await api.createIssue(title, {
      description: Array.from({ length: 45 }, (_, i) => `Description paragraph ${i}. A long description precedes the timeline.`).join("\n\n"),
    });
    for (let i = 0; i < 24; i++) {
      await api.createComment(issue.id, `Outer comment ${i}\n\n${"Before the long thread. ".repeat(5 + i % 8)}`);
    }
    const root = await api.createComment(issue.id, "Long discussion root");
    let targetId = "";
    for (let i = 0; i < 80; i++) {
      const reply = await api.createComment(issue.id, i === 61
        ? "Notification reply 61\n\nThe notification should land here."
        : `Reply ${i}\n\n${Array.from({ length: 2 + i % 11 }, (_, j) => `Paragraph ${j}. ${"Variable-height Markdown content. ".repeat(3)}`).join("\n\n")}`, root.id);
      if (i === 61) targetId = reply.id;
    }
    for (let i = 0; i < 4; i++) await api.createComment(issue.id, `After thread ${i}`);
    await api.seedCommentNotification(issue.id, targetId, title);
    await page.goto(`/${slug}/inbox`);
    const notification = page.getByText(title, { exact: true }).first();
    await notification.click();
    const target = page.locator(`#comment-${targetId}`);
    await expect(target).toBeVisible();
    await expect.poll(() => readingPosition(page, targetId).then((p) => p?.mounted)).toBe(true);
    await expect.poll(() => readingPosition(page, targetId).then((p) => p?.visible)).toBe(true);
    const before = (await readingPosition(page, targetId))!;

    const leaveAndReturn = async () => {
      await page.locator(`a[href="/${slug}/issues"]`).first().click();
      await expect(page).toHaveURL(new RegExp(`/${slug}/issues`));
      await page.goBack();
    };
    const expectRestored = async (position: { id: string; offset: number }) => {
      let stable = 0;
      await expect.poll(async () => {
        const current = await readingPosition(page, position.id);
        stable = current?.mounted && current.visible && Math.abs(current.offset - position.offset) < 4 ? stable + 1 : 0;
        return stable;
      }, { timeout: 10_000, intervals: [100] }).toBeGreaterThanOrEqual(3);
    };

    await page.screenshot({ path: testInfo.outputPath("before-leaving.png"), animations: "disabled" });
    await leaveAndReturn();
    await expectRestored(before);
    await page.screenshot({ path: testInfo.outputPath("restored-notification.png"), animations: "disabled" });

    // Reading farther up must restore that reply, rather than replay the notification.
    const container = page.locator(`[data-tab-scroll-root="main:${issue.id}"]`);
    await container.hover();
    await page.mouse.wheel(0, -1800);
    await expect.poll(async () => (await readingPosition(page))?.id).not.toBe(targetId);
    const reading = (await readingPosition(page))!;
    expect(reading).not.toBeNull();
    await leaveAndReturn();
    await expectRestored(reading);
    await page.locator(`a[href="/${slug}/issues"]`).first().click();
    await expect(page).toHaveURL(new RegExp(`/${slug}/issues`));
    await page.setViewportSize({ width: 1200, height: 900 });
    await page.goBack();
    await expectRestored(reading);
    expect(await page.locator('[data-virtual-thread-mounted="true"]').count()).toBeLessThan(20);
    await page.screenshot({ path: testInfo.outputPath("restored-reading-position.png"), animations: "disabled" });

    // An explicit click still replays the original notification after a restore.
    await notification.click();
    await expect.poll(() => readingPosition(page, targetId).then((p) => p?.visible)).toBe(true);
  } finally {
    await api.cleanup();
  }
});
