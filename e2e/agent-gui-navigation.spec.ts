import { expect, test } from "@playwright/test";
import "./env";

for (const theme of ["light", "dark"] as const) {
  test(`directory count, buffered prefetch and bidirectional paging (${theme})`, async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.setViewportSize({ width: theme === "dark" ? 1000 : 1500, height: 900 });
    await page.goto("/tutti-agent-gui/direct.html?navigation");
    if (theme === "dark") await page.locator("#theme").click();
    await page.locator("#paged").click();
    const ticks = page.locator(".agent-gui-message-locator__tick");
    const timeline = page.getByTestId("agent-gui-timeline");
    const messages = page.locator('[data-agent-transcript-row-speaker="user"]');
    await expect(ticks).toHaveCount(86);
    await expect(messages.getByText("Question 85", { exact: true })).toBeVisible();
    await page.evaluate(() => { (window as any).paging.navigationDelay = 500; });
    await ticks.nth(85).hover();
    const panel = page.getByTestId("agent-message-locator-panel");
    await expect(panel.getByText("Question 66", { exact: true })).toBeVisible();
    const anchorBefore = await panel.getByText("Question 66", { exact: true }).evaluate((element) => element.getBoundingClientRect().top);
    await expect.poll(() => panel.getAttribute("data-item-count").then(Number)).toBeGreaterThanOrEqual(60);
    await expect(panel).not.toHaveAttribute("aria-busy", "true");
    const anchorAfter = await panel.getByText("Question 66", { exact: true }).evaluate((element) => element.getBoundingClientRect().top);
    expect(Math.abs(anchorAfter - anchorBefore)).toBeLessThan(1);
    await page.evaluate(() => { (window as any).paging.navigationDelay = 100; });
    const before = await timeline.evaluate((element) => element.scrollTop);
    const rail = page.getByTestId("agent-message-locator-viewport");
    await rail.evaluate((element) => { element.scrollTop = 0; });
    await ticks.nth(0).hover();
    await expect(ticks.nth(0)).toHaveAttribute("title", "Question 0");
    await expect.poll(() => panel.getAttribute("data-item-count").then(Number)).toBeGreaterThanOrEqual(61);
    await expect(panel.getByText("Question 0", { exact: true })).toBeInViewport();
    await expect(panel).not.toHaveAttribute("aria-busy", "true");
    expect(await timeline.evaluate((element) => element.scrollTop)).toBe(before);
    await expect(messages.getByText("Question 10", { exact: true })).toHaveCount(0);
    await panel.hover();
    await page.mouse.wheel(0, 5000);
    await expect(panel).toHaveAttribute("data-item-count", "86");
    expect(await panel.locator("button").count()).toBeLessThan(120);
    const paths = await page.evaluate(() => (window as any).paging.navigation as string[]);
    expect(paths.every((path) => path.includes("include_body=false"))).toBe(true);
    expect(paths.filter((path) => path.includes("limit=")).every((path) => path.includes("limit=20"))).toBe(true);
    expect(await timeline.evaluate((element) => element.scrollTop)).toBe(before);
    expect(await page.evaluate(() => (window as any).paging.reads)).toBe(0);
    await page.screenshot({ path: `.tmp/agent-gui-navigation/${theme}-hover-list.png` });
    await page.mouse.wheel(0, -1600);
    await panel.getByText("Question 10", { exact: true }).click();
    await expect(messages.getByText("Question 10", { exact: true })).toBeInViewport();
    await timeline.hover({ position: { x: 20, y: 100 } });
    await page.mouse.wheel(0, -50000);
    await expect.poll(() => page.evaluate(() => (window as any).paging.navigation.some((path: string) => path.includes("before_index=6") && !path.includes("include_body=false")))).toBe(true);
    await page.mouse.wheel(0, 50000);
    await expect.poll(() => page.evaluate(() => (window as any).paging.navigation.some((path: string) => path.includes("after_index=15") && !path.includes("include_body=false")))).toBe(true);
    await expect(ticks).toHaveCount(86);
    expect(errors).toEqual([]);
  });
}

test("pauses failed prefetch and resumes after an explicit retry", async ({ page }) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await page.goto("/tutti-agent-gui/direct.html?navigation");
  await page.locator("#paged").click();
  const ticks = page.locator(".agent-gui-message-locator__tick");
  await expect(ticks).toHaveCount(86);
  await page.evaluate(() => { (window as any).paging.failNavigation = true; });
  await ticks.nth(85).hover();
  const alert = page.getByRole("alert");
  await expect(alert).toContainText("Navigation fixture unavailable");
  const failedRequests = await page.evaluate(() => (window as any).paging.navigation.length);
  await page.waitForTimeout(350);
  expect(await page.evaluate(() => (window as any).paging.navigation.length)).toBe(failedRequests);
  await page.evaluate(() => { (window as any).paging.failNavigation = false; });
  await alert.getByRole("button").click();
  await expect(alert).toHaveCount(0);
  await ticks.nth(85).hover();
  await expect.poll(() => page.getByTestId("agent-message-locator-panel").getAttribute("data-item-count").then(Number)).toBeGreaterThanOrEqual(60);
});

if (process.env.MULTICA_NAVIGATION_SCROLL_PROFILE === "1") {
  test("samples continuous panel scrolling with delayed metadata", async ({ page }) => {
    await page.setViewportSize({ width: 1500, height: 900 });
    await page.goto("/tutti-agent-gui/direct.html?navigation&navigationProfile");
    await page.locator("#paged").click();
    const rail = page.getByTestId("agent-message-locator-viewport");
    const panel = page.getByTestId("agent-message-locator-panel");
    await page.locator('.agent-gui-message-locator__tick').last().hover();
    await expect(panel).toHaveAttribute("data-item-count", /^\d+$/);
    await expect.poll(() => panel.getAttribute("data-item-count").then(Number)).toBeGreaterThanOrEqual(60);
    await expect(panel).not.toHaveAttribute("aria-busy", "true");
    await panel.hover();
    await page.evaluate(() => {
      const panel = document.querySelector<HTMLElement>('[data-testid="agent-message-locator-panel"]')!;
      const sample = { frames: 0, blockedAtEdge: 0, frameGaps: [] as number[], last: performance.now(), stopped: false };
      Object.assign(window, { navigationScrollSample: sample });
      const tick = (now: number) => {
        if (sample.stopped) return;
        sample.frames++;
        sample.frameGaps.push(now - sample.last);
        sample.last = now;
        if (panel.scrollTop <= 1 && panel.getAttribute("aria-busy") === "true") sample.blockedAtEdge++;
        requestAnimationFrame(tick);
      };
      requestAnimationFrame(tick);
    });
    for (let index = 0; index < 50; index++) {
      await page.mouse.wheel(0, -60);
      await page.waitForTimeout(16);
    }
    const result = await page.evaluate(() => {
      const sample = (window as any).navigationScrollSample;
      sample.stopped = true;
      const gaps = sample.frameGaps.sort((a: number, b: number) => a - b);
      return { frames: sample.frames, blockedAtEdge: sample.blockedAtEdge, p95FrameGapMs: gaps[Math.floor(gaps.length * .95)], maxFrameGapMs: gaps.at(-1), requests: (window as any).paging.navigation.length };
    });
    console.log("navigation-scroll-sample", JSON.stringify(result));
    await test.info().attach("navigation-scroll-sample", { body: JSON.stringify(result), contentType: "application/json" });
    await expect(rail).toBeVisible();
  });
}
