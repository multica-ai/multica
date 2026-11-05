import { expect,test,type Page } from "@playwright/test";
import "./env";

// The conversation GUI shares a document with the app. Run against a server for
// the harness that scripts/build-e2e-harness.mjs writes to apps/web/public.
const TRACKED = [
  "color",
  "backgroundColor",
  "fontFamily",
  "fontSize",
  "lineHeight",
  "paddingTop",
  "paddingLeft",
  "borderTopWidth",
  "borderTopColor",
  "borderRadius",
  "boxSizing",
  "margin"
] as const;

function appStyles(page: Page) {
  return page.evaluate(
    (tracked) =>
      Object.fromEntries(
        [
          "html",
          "body",
          "#toggle",
          "#theme",
          "#heading",
          "#paragraph",
          '[data-testid="app-aside"]'
        ].map((selector) => {
          const style = getComputedStyle(document.querySelector(selector)!);
          return [
            selector,
            Object.fromEntries(tracked.map((name) => [name, style[name]]))
          ];
        })
      ),
    TRACKED
  );
}

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await page.goto("/tutti-agent-gui/direct.html");
});

test("mounting the GUI changes no app style, and unmounting removes it", async ({
  page
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const before = await appStyles(page);
  await page.locator("#toggle").click();
  await expect(page.getByText("Reply in main-conversation")).toBeVisible();
  expect(await appStyles(page)).toEqual(before);
  await page.screenshot({ path: ".tmp/agent-gui-direct/light.png" });
  await page.locator("#toggle").click();
  await expect(page.getByTestId("agent-gui-root")).toHaveCount(0);
  expect(await appStyles(page)).toEqual(before);
  expect(errors).toEqual([]);
});

test("instances keep their own state in one document and fill their host", async ({
  page
}) => {
  await page.locator("#toggle").click();
  const main = page.locator(
    '[data-instance="main-conversation"] [data-testid="agent-gui-root"]'
  );
  const side = page.locator(
    '[data-instance="side-conversation"] [data-testid="agent-gui-root"]'
  );
  await expect(main.getByText("Reply in main-conversation")).toBeVisible();
  await expect(side.getByText("Reply in side-conversation")).toBeVisible();
  await expect(main).toHaveAttribute("data-side-pane", "false");
  await expect(side).toHaveAttribute("data-side-pane", "true");
  await expect(main.getByText("Reply in side-conversation")).toHaveCount(0);
  const boxes = await Promise.all([main.boundingBox(), side.boundingBox()]);
  for (const box of boxes) expect(box!.height).toBeGreaterThan(800);
  expect(Math.abs(boxes[0]!.width - boxes[1]!.width)).toBeLessThan(2);
});

test("GUI colors follow the theme without restyling the app", async ({
  page
}) => {
  await page.locator("#toggle").click();
  const root = page.getByTestId("agent-gui-root").first();
  await expect(page.getByText("Reply in main-conversation")).toBeVisible();
  const color = () =>
    root.evaluate((element) =>
      getComputedStyle(element).getPropertyValue("--background").trim()
    );
  const light = await color();
  const before = await appStyles(page);
  await page.locator("#theme").click();
  await expect(page.getByTestId("agent-gui-root").first()).toHaveAttribute(
    "data-theme",
    "dark"
  );
  await expect(page.getByText("Reply in main-conversation")).toBeVisible();
  expect(
    await page
      .getByTestId("agent-gui-root")
      .first()
      .evaluate((element) =>
        getComputedStyle(element).getPropertyValue("--background").trim()
      )
  ).not.toBe(light);
  expect(await appStyles(page)).toEqual(before);
  await page.screenshot({ path: ".tmp/agent-gui-direct/dark.png" });
});

test("GUI overlays are styled, though they render outside its root", async ({
  page
}) => {
  await page.locator("#toggle").click();
  await expect(page.getByText("Reply in main-conversation")).toBeVisible();
  await page
    .locator('[data-instance="main-conversation"]')
    .getByText("Default model")
    .locator("visible=true")
    .first()
    .click();
  const portal = page.locator("[data-tutti-portal-root]");
  await expect(portal).toHaveClass(/multica-agent-gui/);
  const overlay = portal.locator("*").first();
  await expect(overlay).toBeVisible();
  const background = await portal
    .locator(
      "[role=menu], [role=listbox], [role=dialog], [data-slot*=content], [data-slot*=surface]"
    )
    .first()
    .evaluate((element) => getComputedStyle(element).backgroundColor);
  expect(background).not.toBe("rgba(0, 0, 0, 0)");
  await page.screenshot({ path: ".tmp/agent-gui-direct/overlay.png" });
});

const paging = (page: Page) =>
  page.evaluate(
    () =>
      (window as unknown as { paging: { reads: number; started: number[] } })
        .paging
  );
const scroller = (page: Page) =>
  page.getByText("Answer 59").evaluate((node) => {
    for (
      let element = node.parentElement;
      element;
      element = element.parentElement
    ) {
      const overflow = getComputedStyle(element).overflowY;
      if (
        (overflow === "auto" || overflow === "scroll") &&
        element.scrollHeight > element.clientHeight
      ) {
        element.setAttribute("data-test-scroller", "true");
        return true;
      }
    }
    return false;
  });

test("shows five turns before supplementing a seven-turn conversation without virtualization", async ({ page }) => {
  await page.goto("/tutti-agent-gui/direct.html?small=1");
  await page.evaluate(() => { (window as unknown as { paging: { historyDelay: number } }).paging.historyDelay = 1_000; });
  await page.locator("#paged").click();
  await expect(page.getByText("Answer 6", { exact: false })).toBeVisible();
  const first = page.getByRole("paragraph").filter({ hasText: /^Question 0$/ });
  await expect(first).toHaveCount(0);
  await expect(first).toBeAttached();
  expect((await paging(page)).started).toEqual([2]);
  expect((await paging(page)).reads).toBe(1);
  await expect(page.locator('[data-agent-transcript-virtualized="true"]')).toHaveCount(0);
});

test("a long conversation opens at its end with three screens of history above, and pages to its start", async ({
  page
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.locator("#paged").click();
  await expect(page.getByText("Answer 59", { exact: false })).toBeVisible();
  expect(await scroller(page)).toBe(true);
  const area = page.locator("[data-test-scroller]");
  // It keeps fetching until three screens sit above the view, then stops.
  await expect
    .poll(async () =>
      area.evaluate((element) => element.scrollTop >= 3 * element.clientHeight)
    )
    .toBe(true);
  const settled = (await paging(page)).reads;
  await page.waitForTimeout(400);
  expect((await paging(page)).reads).toBe(settled);
  expect(settled).toBeLessThan(6);
  await expect(page.getByText("Answer 59", { exact: false })).toBeInViewport();

  // Scrolling up keeps what is on screen in place while older turns arrive above it.
  // User intent detaches follow mode before programmatic positioning measures paging.
  await area.hover();
  await page.mouse.wheel(0, -10000);
  await page.waitForTimeout(100);
  for (
    let i = 0;
    i < 40 &&
    !(await page
      .getByText("Question 0", { exact: true })
      .isVisible()
      .catch(() => false));
    i++
  ) {
    await area.evaluate((element) => {
      element.scrollTop = 0;
      element.dispatchEvent(new Event("scroll"));
    });
    await page.waitForTimeout(80);
  }
  await expect(page.getByText("Question 0", { exact: true })).toBeVisible();
  const reads = (await paging(page)).reads;
  await area.evaluate((element) => {
    element.scrollTop = 0;
    element.dispatchEvent(new Event("scroll"));
  });
  await page.waitForTimeout(300);
  expect((await paging(page)).reads).toBe(reads);
  expect(new Set((await paging(page)).started).size).toBe(
    (await paging(page)).started.length
  );
  expect(errors).toEqual([]);
  await page.screenshot({ path: ".tmp/agent-gui-direct/paged-top.png" });
  await page
    .getByTestId("agent-message-locator")
    .getByRole("button", { name: "Question 59", exact: true })
    .click();
  await expect(
    page.getByRole("paragraph").filter({ hasText: /^Question 59$/ })
  ).toBeInViewport();
});

test("a failing history read does not retry by itself, and scrolling asks again", async ({
  page
}) => {
  await page.goto("/tutti-agent-gui/direct.html?failOlder=1");
  await page.locator("#paged").click();
  await expect(page.getByText("Answer 59", { exact: false })).toBeVisible();
  await page.getByTestId("agent-gui-timeline").hover();
  await page.mouse.wheel(0, -10000);
  await expect.poll(async () => (await paging(page)).reads).toBeGreaterThan(0);
  await page.waitForTimeout(600);
  const reads = (await paging(page)).reads;
  expect(reads).toBeLessThan(3);
  await page.waitForTimeout(400);
  expect((await paging(page)).reads).toBe(reads);
  expect(await scroller(page)).toBe(true);
  await page.locator("[data-test-scroller]").hover();
  await page.mouse.wheel(0, 300);
  await page.waitForTimeout(50);
  await page.mouse.wheel(0, -300);
  await expect
    .poll(async () => (await paging(page)).reads)
    .toBeGreaterThan(reads);
});

test("expands and collapses tool work while keeping the final reply in view", async ({
  page
}) => {
  await page.locator("#paged").click();
  await expect(page.getByText("Answer 59", { exact: false })).toBeVisible();
  const expand = page.getByRole("button", {
    name: "Expand task details",
    exact: true
  });
  await expect(expand).toBeVisible();
  const tool = page.getByRole("button", {
    name: "Run command Completed fixture-inspect",
    exact: true
  });
  await expect(tool).not.toBeVisible();
  await expand.click();
  await page.getByRole("button", { name: "1 tool calls", exact: true }).click();
  await expect(tool).toBeVisible();
  await expect(page.getByText("Answer 59", { exact: false })).toBeInViewport();
  await page
    .getByRole("button", { name: "Collapse task details", exact: true })
    .click();
  await expect(expand).toBeVisible();
  await expect(tool).not.toBeVisible();
  await expect(page.getByText("Answer 59", { exact: false })).toBeInViewport();
});

test("keeps streaming content at the measured end and preserves scroll-away intent", async ({ page }) => {
  await page.goto("/tutti-agent-gui/performance.html");
  await expect(page.getByText("Live reply", { exact: true })).toBeVisible();
  const timeline = page.getByTestId("agent-gui-timeline");
  const gap = () => timeline.evaluate((element) =>
    element.scrollHeight - element.clientHeight - element.scrollTop
  );
  const stream = (count: number) => page.evaluate(async (count) => {
    const fixture = (window as Window & {
      conversationFixture: {
        stream(count: number, rate: number): Promise<{ delivered: number }>;
      };
    }).conversationFixture;
    const { delivered } = await fixture.stream(count, 120);
    const token = `streamed-token-${String(delivered).padStart(4, "0")}`;
    while (!document.body.textContent?.includes(token)) {
      await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    }
    await new Promise<void>((resolve) => requestAnimationFrame(() =>
      requestAnimationFrame(() => resolve())
    ));
  }, count);
  await expect.poll(gap).toBeLessThanOrEqual(1);

  const incoming = stream(180);
  await page.locator('[contenteditable="true"]').first().pressSequentially(
    "Typing while the reply grows.", { delay: 8 }
  );
  await incoming;
  await expect.poll(gap).toBeLessThanOrEqual(1);

  await timeline.hover();
  await page.mouse.wheel(0, -700);
  await expect.poll(gap).toBeGreaterThan(100);
  await expect.poll(() => timeline.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  await page.waitForTimeout(200);
  const detachedTop = await timeline.evaluate((element) => element.scrollTop);
  await stream(90);
  expect(await timeline.evaluate((element) => element.scrollTop)).toBeCloseTo(detachedTop, 0);

  await page.getByTestId("agent-gui-scroll-to-bottom").click();
  await expect.poll(gap).toBeLessThanOrEqual(1);
  await stream(90);
  await expect.poll(gap).toBeLessThanOrEqual(1);
});

test("slash host commands survive capability refresh and use the existing settings and Side bridge", async ({ page }) => {
  await page.locator("#toggle").click();
  const main = page.locator('[data-instance="main-conversation"]');
  const editor = main.locator('[contenteditable="true"]');
  await expect(main.getByText("Reply in main-conversation")).toBeVisible();
  const command = (name: string) => page.getByTestId(`agent-gui-composer-slash-command-${name}`);
  await editor.fill("/");
  for (const name of ["side", "reasoning", "model", "status"]) await expect(command(name)).toBeVisible();
  const palette = page.getByTestId("agent-gui-slash-palette-surface");
  const fullHeight = (await palette.boundingBox())!.height;
  expect(fullHeight).toBeLessThan(210);
  // The request above refreshes the empty provider catalog. Host commands must
  // still be present when that async refresh settles.
  await expect(page.getByRole("status", { name: "Loading capabilities" })).toHaveCount(0);
  await command("model").click();
  await page.getByRole("menuitemradio", { name: "Fixture model", exact: true }).click();
  await expect(main.getByTestId("multica-model-picker")).toContainText("Fixture model");
  await expect(editor).toHaveText("");
  await expect(page.locator(".multica-model-picker-menu")).toBeHidden();
  await expect(editor).toBeFocused();
  await editor.fill("/rea");
  await expect(command("reasoning")).toBeVisible();
  await expect(command("model")).toHaveCount(0);
  const filteredHeight = (await palette.boundingBox())!.height;
  expect(filteredHeight).toBeLessThan(100);
  expect(filteredHeight).toBeLessThan(fullHeight);
  await command("reasoning").click();
  await page.getByRole("menuitemradio", { name: "High", exact: true }).click();
  await expect(main.getByTestId("multica-model-picker")).toContainText("High");
  await expect(page.locator(".multica-model-picker-menu")).toBeHidden();
  await expect(editor).toBeFocused();
  await editor.fill("/status");
  await editor.press("Escape");
  await editor.press("Enter");
  await expect(page.getByText("Conversation ID: main-conversation", { exact: true })).toBeVisible();
  await expect(page.getByText("100 / 1,000 tokens", { exact: true })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator(".multica-usage-popover")).toBeHidden();
  await editor.click();
  await editor.fill("/");
  await command("side").click();
  await expect.poll(() => page.evaluate(() => (window as unknown as { hostActions: { method: string; params: { sessionId?: string } }[] }).hostActions.filter((call) => call.method === "host.openSide").map((call) => call.params.sessionId))).toEqual(["main-conversation"]);
  await expect(editor).toHaveText("");
  const sends = await page.evaluate(() => (window as unknown as { hostActions: { method: string }[] }).hostActions.filter((call) => call.method === "session.send"));
  expect(sends).toEqual([]);
  await editor.fill("/");
  for (const theme of ["light", "dark"]) {
    if (theme === "dark") {
      await page.locator("#theme").click();
      await editor.fill("/");
      await editor.click();
    }
    await expect(command("model")).toBeVisible();
    await page.screenshot({ path: `.tmp/agent-gui-direct/slash-${theme}.png` });
  }
});
