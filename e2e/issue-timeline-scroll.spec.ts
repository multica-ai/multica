import { test, expect, type Page } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";

async function readingPosition(page: Page, commentId?: string) {
  return page.evaluate((id) => {
    const container = document.querySelector<HTMLElement>('[data-tab-scroll-root^="main:"]');
    if (!container) return null;
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
    const waitForSettledPosition = async (commentId?: string) => {
      let last: { id: string; offset: number } | null = null;
      let stable = 0;
      await expect.poll(async () => {
        const current = await readingPosition(page, commentId);
        if (!current?.mounted || !current.visible) {
          last = null;
          stable = 0;
          return 0;
        }
        if (last && last.id === current.id && Math.abs(current.offset - last.offset) < 2) {
          stable += 1;
        } else {
          stable = 1;
        }
        last = { id: current.id, offset: current.offset };
        return stable;
      }, { timeout: 10_000, intervals: [100] }).toBeGreaterThanOrEqual(3);
      return last!;
    };
    const before = await waitForSettledPosition(targetId);

    const leaveAndReturn = async () => {
      await page.locator(`a[href="/${slug}/issues"]`).first().click();
      await expect(page).toHaveURL((url) => url.pathname === `/${slug}/issues`);
      await expect(page.getByRole("heading", { name: "All issues" })).toBeVisible();
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
    const reading = await waitForSettledPosition();
    expect(reading).not.toBeNull();
    await leaveAndReturn();
    await expectRestored(reading);
    await page.locator(`a[href="/${slug}/issues"]`).first().click();
    await expect(page).toHaveURL((url) => url.pathname === `/${slug}/issues`);
    await expect(page.getByRole("heading", { name: "All issues" })).toBeVisible();
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

// Scroll the timeline up in fixed steps and record how far a visible reply
// moved beyond the step itself. Anything but ~0 is content jumping under the
// reader: an offscreen reply above the viewport swapped its placeholder
// height for its real one without the scroll position following it.
async function scrollUpShifts(page: Page, steps: number, delta = 150) {
  const shifts: number[] = [];
  for (let i = 0; i < steps; i++) {
    const shift = await page.evaluate(async (d) => {
      const container = document.querySelector<HTMLElement>('[data-tab-scroll-root^="main:"]')!;
      const c = container.getBoundingClientRect();
      // The first comment whose top edge is on screen. Its own resize cannot
      // move its top, so only layout changes above it show up as a shift.
      const ref = [...container.querySelectorAll<HTMLElement>('[id^="comment-"]')]
        .find((el) => { const r = el.getBoundingClientRect(); return r.top > c.top + 80 && r.top < c.bottom - 80; });
      const before = ref?.getBoundingClientRect().top;
      const scrollBefore = container.scrollTop;
      container.scrollTop = scrollBefore - d;
      const applied = scrollBefore - container.scrollTop;
      for (let f = 0; f < 10; f++) await new Promise((r) => requestAnimationFrame(r));
      await new Promise((r) => setTimeout(r, 60));
      return ref?.isConnected && before !== undefined ? ref.getBoundingClientRect().top - before - applied : null;
    }, delta);
    if (shift !== null) shifts.push(Math.round(shift));
  }
  return shifts;
}

test("scrolling up a virtualized long thread keeps the reading position steady", async ({ page }) => {
  test.setTimeout(180_000);
  await page.setViewportSize({ width: 1440, height: 900 });
  const api = await createTestApi();
  try {
    const slug = await loginAsDefault(page);
    const title = "Long-thread upward scroll stability";
    // Two shapes that trip Virtuoso's own compensation differently: one-line
    // replies ending the timeline, and mixed heights followed by more comments.
    // Both are mostly far shorter than an unmeasured row's estimate.
    const seedThread = async (issueTitle: string, mixed: boolean) => {
      const issue = await api.createIssue(issueTitle, { description: "Short description." });
      for (let i = 0; i < 5; i++) await api.createComment(issue.id, `Outer comment ${i}`);
      const root = await api.createComment(issue.id, "Long discussion root");
      let targetId = "";
      for (let i = 0; i < 120; i++) {
        const body = mixed && i % 17 === 0
          ? Array.from({ length: 12 }, (_, j) => `Paragraph ${j}. ${"Long reply content. ".repeat(6)}`).join("\n\n")
          : `Reply ${i}. Short answer.`;
        const reply = await api.createComment(issue.id, i === 100 ? "Notification reply 100" : body, root.id);
        if (i === 100) targetId = reply.id;
      }
      if (mixed) for (let i = 0; i < 4; i++) await api.createComment(issue.id, `After thread ${i}`);
      return { issue, targetId };
    };
    const short = await seedThread(title, false);
    const mixed = await seedThread("Long-thread mixed heights", true);
    const targetId = short.targetId;
    await api.seedCommentNotification(short.issue.id, targetId, title);

    // 1. Land on a reply deep in the thread from Inbox, then read upward.
    await page.goto(`/${slug}/inbox`);
    await page.getByText(title, { exact: true }).first().click();
    await expect(page.locator(`#comment-${targetId}`)).toBeVisible();
    await expect.poll(() => readingPosition(page, targetId).then((p) => p?.mounted && p.visible), { timeout: 10_000 }).toBe(true);
    await page.waitForTimeout(1000);
    const fromInbox = await scrollUpShifts(page, 40);
    expect(fromInbox.length).toBeGreaterThan(30);
    expect.soft(fromInbox.filter((s) => Math.abs(s) > 2), `Inbox landing shifts: ${fromInbox}`).toEqual([]);

    // 2. Open the issue normally, go to the end of the discussion, read upward.
    await page.goto(`/${slug}/issues/${mixed.issue.identifier}`);
    await expect(page.getByText("Long discussion root")).toBeVisible();
    const toEnd = () => page.evaluate(() => {
      const c = document.querySelector<HTMLElement>('[data-tab-scroll-root^="main:"]')!;
      c.scrollTop = c.scrollHeight;
    });
    await toEnd();
    await page.waitForTimeout(800);
    await toEnd();
    await page.waitForTimeout(800);
    const fromEnd = await scrollUpShifts(page, 40);
    expect(fromEnd.length).toBeGreaterThan(30);
    expect.soft(fromEnd.filter((s) => Math.abs(s) > 2), `End-of-thread shifts: ${fromEnd}`).toEqual([]);
    expect(await page.locator('[data-virtual-thread-mounted="true"]').count()).toBeLessThan(60);
  } finally {
    await api.cleanup();
  }
});

test("Inbox landing survives a comment that arrives while it is still aligning", async ({ page }) => {
  test.setTimeout(180_000);
  await page.setViewportSize({ width: 1440, height: 900 });
  const api = await createTestApi();
  try {
    const slug = await loginAsDefault(page);
    const title = "Inbox landing during realtime updates";
    const body = (lines: number) => Array.from({ length: lines }, (_, j) => `Line ${j}. ${"Agent reply text that wraps. ".repeat(4)}`).join("\n\n");
    const issue = await api.createIssue(title, { description: body(8) });
    // The shape of a busy agent issue: two threads of nested member/agent
    // exchanges, the notification on the newest agent reply.
    const first = await api.createComment(issue.id, "First thread root");
    let parent = first;
    for (let i = 0; i < 4; i++) parent = await api.createComment(issue.id, i % 2 ? "Follow-up" : body(20), parent.id);
    const second = await api.createComment(issue.id, "Second thread root");
    let targetId = "";
    for (let i = 0; i < 7; i++) {
      const ask = await api.createComment(issue.id, `Member ask ${i}`, second.id);
      targetId = (await api.createComment(issue.id, body(5 + (i % 4) * 5), ask.id)).id;
    }
    const other = await api.createIssue("Another notification", { description: "Short." });
    await api.seedCommentNotification(other.id, (await api.createComment(other.id, "Other comment")).id, "Another notification");
    await api.seedCommentNotification(issue.id, targetId, title);

    await page.goto(`/${slug}/inbox`);
    // A realtime comment re-renders the timeline mid-landing. Before MUL-9068
    // that cancelled the alignment for good, or aligned a replaced row that
    // measured as zero and scrolled the detail back to the top.
    for (const delay of [0, 120, 240, 360]) {
      await page.getByText(title, { exact: true }).first().click();
      await page.locator(`[data-tab-scroll-root="main:${issue.id}"]`).waitFor();
      await page.waitForTimeout(delay);
      await api.createComment(issue.id, `Arrived after ${delay}ms`, first.id);
      let stable = 0;
      await expect.poll(async () => {
        const p = await readingPosition(page, targetId);
        stable = p?.mounted && p.visible ? stable + 1 : 0;
        return stable;
      }, { message: `landing after a ${delay}ms update`, timeout: 10_000, intervals: [200] }).toBeGreaterThanOrEqual(5);
      await page.getByText("Another notification", { exact: true }).first().click();
      await expect(page.getByText("Other comment")).toBeVisible();
    }
  } finally {
    await api.cleanup();
  }
});

test("a notification under a long description lands on its reply, and again from another page", async ({ page }) => {
  test.setTimeout(180_000);
  await page.setViewportSize({ width: 1100, height: 620 });
  const api = await createTestApi();
  try {
    const slug = await loginAsDefault(page);
    const title = "Landing below a long description";
    // MUL-9492: the description alone is several viewports tall, so the
    // timeline starts far below the fold and Virtuoso has measured nothing yet.
    const description = Array.from({ length: 7 }, (_, i) => [
      `## Section ${i}`,
      `Opening paragraph ${i}, see [MUL-${9000 + i}](mention://issue/01a1e252-c081-7b00-b653-${String(100000000000 + i)}). ${"The description runs long. ".repeat(4)}`,
      ...Array.from({ length: 6 }, (_, j) => `- Point ${i}.${j}: ${"a clause that wraps. ".repeat(3)}`),
      ["| Term | Value | Notes |", "|---|---|---|", ...Array.from({ length: 4 }, (_, j) => `| Row ${j} | ${j * 25}% | ${"A table cell that wraps. ".repeat(3)} |`)].join("\n"),
    ].join("\n\n")).join("\n\n");
    const issue = await api.createIssue(title, { description });
    const first = await api.createComment(issue.id, "First thread root");
    await api.createComment(issue.id, `Short reply. ${"Some context. ".repeat(10)}`, first.id);
    const root = await api.createComment(issue.id, "Long discussion root");
    let targetId = "";
    for (let i = 0; i < 24; i++) {
      const body = Array.from({ length: 3 + (i % 5) }, (_, j) => `Paragraph ${j}. ${"A long agent reply in a long thread. ".repeat(8)}`).join("\n\n");
      targetId = (await api.createComment(issue.id, `Reply ${i}\n\n${body}`, root.id)).id;
    }
    const other = await api.createIssue("Another notification", { description: "Short." });
    await api.seedCommentNotification(other.id, (await api.createComment(other.id, "Other comment")).id, "Another notification");
    await api.seedCommentNotification(issue.id, targetId, title);

    const landed = () => page.evaluate((id) => {
      const container = [...document.querySelectorAll<HTMLElement>('[data-tab-scroll-root^="main:"]')].find((el) => el.clientHeight > 0);
      const target = container?.querySelector<HTMLElement>(`[id="comment-${id}"]`);
      if (!container || !target || target.dataset.virtualThreadMounted === "false") return null;
      return Math.round(target.getBoundingClientRect().top - container.getBoundingClientRect().top);
    }, targetId);
    const expectLanded = async (message: string) => {
      let stable = 0;
      await expect.poll(async () => {
        const offset = await landed();
        stable = offset !== null && offset >= 0 && offset < 120 ? stable + 1 : 0;
        return stable;
      }, { message, timeout: 10_000, intervals: [100] }).toBeGreaterThanOrEqual(5);
    };

    await page.goto(`/${slug}/inbox`);
    const notification = page.getByText(title, { exact: true }).first();
    await notification.click();
    await expectLanded("first click");

    // Read elsewhere, leave, and click the same notification from another issue.
    await page.evaluate(() => {
      const container = [...document.querySelectorAll<HTMLElement>('[data-tab-scroll-root^="main:"]')].find((el) => el.clientHeight > 0)!;
      container.scrollTop = 1500;
    });
    await page.getByText("Another notification", { exact: true }).first().click();
    await expect(page.getByText("Other comment")).toBeVisible();
    await notification.click();
    await expectLanded("click from another issue");
  } finally {
    await api.cleanup();
  }
});
