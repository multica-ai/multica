import { expect, test, type Page } from "@playwright/test";
import { chatScrollScript } from "../packages/views/test/chat-scroll-bundle.mjs";

declare global {
  interface Window {
    chatScrollFixture: {
      render(): void;
      grow(px: number): void;
      append(): void;
      prepend(): void;
      pin(): void;
      landOldPin(): void;
      startStream(): void;
      stopStream(): void;
    };
  }
}

const position = (page: Page) => page.locator("#chat-scroll").evaluate((el) => el.scrollTop);
const distance = (page: Page) => page.locator("#chat-scroll").evaluate((el) =>
  el.scrollHeight - el.scrollTop - el.clientHeight);

for (const dpr of [1, 1.25, 2]) {
  test.describe(`chat scroll with real Virtuoso (DPR ${dpr})`, () => {
    test.use({ deviceScaleFactor: dpr });
    test.beforeEach(async ({ page }) => {
      await page.setContent('<!doctype html><html><body style="margin:0"><div id="mount"></div></body></html>');
      await page.addScriptTag({ content: chatScrollScript });
      await page.evaluate(() => window.chatScrollFixture.render());
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-ready", "true");
      await expect.poll(() => distance(page)).toBeLessThanOrEqual(4);
    });

    test("fractional wheels release during streaming and stay released through resize and append", async ({ page }) => {
      await page.evaluate(() => window.chatScrollFixture.startStream());
      await page.mouse.move(700, 300);
      for (let i = 0; i < 20; i++) {
        await page.mouse.wheel(0, -0.5);
        await page.evaluate(() => new Promise(requestAnimationFrame));
      }
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-follow-disabled", "true");
      await page.evaluate(async () => {
        window.chatScrollFixture.stopStream();
        // Drain the final chunk's measurement and the last native wheel.
        await new Promise(requestAnimationFrame);
        await new Promise(requestAnimationFrame);
      });
      const parked = await position(page);
      await page.evaluate(() => {
        window.chatScrollFixture.grow(120);
        window.chatScrollFixture.append();
        document.getElementById("chat-scroll")!.style.height = "500px";
      });
      // Cross both Virtuoso's 150ms retry window and its 1200ms request lifetime.
      await page.waitForTimeout(1300);
      expect(Math.abs(await position(page) - parked)).toBeLessThanOrEqual(1);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");
    });

    test("a late bottom request preserves the reader's latest position", async ({ page }) => {
      await page.mouse.move(700, 300);
      await page.mouse.wheel(0, -200);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");
      const first = await position(page);
      await page.mouse.wheel(0, -100);
      await expect.poll(() => position(page)).toBeLessThan(first - 90);
      const latest = await position(page);
      // The same public request used by Virtuoso's queued LAST retries.
      await page.evaluate(() => window.chatScrollFixture.landOldPin());
      await expect.poll(async () => Math.abs(await position(page) - latest)).toBeLessThanOrEqual(1);
      await page.waitForTimeout(300);
      expect(Math.abs(await position(page) - latest)).toBeLessThanOrEqual(1);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");

      const anchor = await page.locator("#chat-scroll").evaluate((el) => {
        const top = el.getBoundingClientRect().top;
        const row = Array.from(el.querySelectorAll<HTMLElement>("[data-item-index]"))
          .find((item) => item.getBoundingClientRect().bottom > top)!;
        return { index: row.dataset.itemIndex!, top: row.getBoundingClientRect().top - top };
      });
      await page.evaluate(() => window.chatScrollFixture.prepend());
      // The virtualizer estimates unmeasured older rows, so scrollTop can
      // change again as it measures. The reader's row and offset must hold.
      await expect.poll(() => position(page)).toBeGreaterThan(latest + 1700);
      await page.evaluate(() => window.chatScrollFixture.landOldPin());
      await expect.poll(() => page.locator("#chat-scroll").evaluate((el, anchor) => {
        const row = el.querySelector<HTMLElement>(`[data-item-index="${anchor.index}"]`);
        return row ? Math.abs(row.getBoundingClientRect().top - el.getBoundingClientRect().top - anchor.top) : Infinity;
      }, anchor)).toBeLessThanOrEqual(1);
    });

    test("PageUp from a row button cancels an outstanding bottom request", async ({ page }) => {
      await page.evaluate(() => {
        window.chatScrollFixture.grow(21);
        window.chatScrollFixture.pin();
      });
      await page.locator("button").last().focus();
      const before = await position(page);
      await page.keyboard.press("PageUp");
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");
      // Let the native key's smooth scroll finish before checking stability.
      await page.waitForTimeout(350);
      const parked = await position(page);
      expect(parked).toBeLessThan(before - 300);
      for (let i = 0; i < 4; i++) {
        await page.evaluate(() => window.chatScrollFixture.grow(31));
        await page.waitForTimeout(60);
      }
      await page.waitForTimeout(1300);
      expect(Math.abs(await position(page) - parked)).toBeLessThanOrEqual(1);
      expect(await distance(page)).toBeGreaterThan(120);
    });

    test("unattributed navigation keeps its position and becomes the latest reader anchor", async ({ page }) => {
      await page.mouse.move(700, 300);
      await page.mouse.wheel(0, -1200);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");
      // Expire the upward input candidate before navigating in that direction.
      await page.waitForTimeout(350);
      const upAnchor = await page.locator("#chat-scroll").evaluate((el) => {
        el.scrollBy(0, -300);
        const top = el.getBoundingClientRect().top;
        const row = Array.from(el.querySelectorAll<HTMLElement>("[data-item-index]"))
          .find((item) => item.getBoundingClientRect().bottom > top)!;
        return { index: row.dataset.itemIndex!, offset: row.getBoundingClientRect().top - top };
      });
      await page.waitForTimeout(50);
      await page.evaluate(() => window.chatScrollFixture.grow(23));
      // Measuring newly exposed rows can change scrollTop; the logical view holds.
      await expect.poll(() => page.locator("#chat-scroll").evaluate((el, anchor) => {
        const row = el.querySelector<HTMLElement>(`[data-item-index="${anchor.index}"]`);
        return row ? Math.abs(row.getBoundingClientRect().top - el.getBoundingClientRect().top - anchor.offset) : Infinity;
      }, upAnchor)).toBeLessThanOrEqual(1);
      const target = await page.locator("#chat-scroll").evaluate((el) => {
        el.scrollBy(0, 300);
        return el.scrollTop;
      });
      await page.waitForTimeout(200);
      expect(Math.abs(await position(page) - target)).toBeLessThanOrEqual(1);
      // A genuinely late LAST request must restore the new navigation target.
      await page.evaluate(() => window.chatScrollFixture.landOldPin());
      await expect.poll(async () => Math.abs(await position(page) - target)).toBeLessThanOrEqual(1);
      // Navigation also wins after a recovery, while its retry may be pending.
      const nextTarget = await page.locator("#chat-scroll").evaluate((el) => {
        el.scrollBy(0, 300);
        return el.scrollTop;
      });
      await page.waitForTimeout(200);
      await page.evaluate(() => window.chatScrollFixture.grow(23));
      await page.waitForTimeout(200);
      expect(Math.abs(await position(page) - nextTarget)).toBeLessThanOrEqual(1);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");
    });

    test("Tab navigation to the last row remains available immediately after release", async ({ page }) => {
      await page.mouse.move(700, 300);
      await page.mouse.wheel(0, -300);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");
      await page.locator("input").last().evaluate((el) => el.focus({ preventScroll: true }));
      // Chromium includes the nested code scroller in keyboard navigation.
      await page.keyboard.press("Tab");
      await expect(page.locator("pre").last()).toBeFocused();
      await page.keyboard.press("Tab");
      await expect(page.locator("[data-row-end]").last()).toBeFocused();
      await expect.poll(() => distance(page)).toBeLessThanOrEqual(4);
      await page.evaluate(() => window.chatScrollFixture.grow(80));
      await expect.poll(() => distance(page)).toBeGreaterThan(70);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");
    });

    test("unattributed navigation to the bottom remains available after the old request expires", async ({ page }) => {
      await page.mouse.move(700, 300);
      await page.mouse.wheel(0, -300);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");
      // A size change inside the window must not extend its deadline.
      await page.waitForTimeout(700);
      await page.evaluate(() => window.chatScrollFixture.grow(13));
      await page.waitForTimeout(600);
      await page.locator("#chat-scroll").evaluate((el) => { el.scrollTop = el.scrollHeight; });
      await expect.poll(() => distance(page)).toBeLessThanOrEqual(4);
      const parked = await position(page);
      await page.evaluate(() => window.chatScrollFixture.grow(30));
      await page.waitForTimeout(200);
      expect(Math.abs(await position(page) - parked)).toBeLessThanOrEqual(1);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");
    });

    for (const modifier of ["Control", "Meta"]) {
      test(`${modifier}+F allows browser navigation to the bottom inside the recovery window`, async ({ page }) => {
        await page.locator("#chat-scroll").focus();
        await page.mouse.move(700, 300);
        await page.mouse.wheel(0, -300);
        await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");
        await page.keyboard.press(`${modifier}+f`);
        // Simulate the browser revealing a match; its find UI is outside the DOM.
        await page.locator("#chat-scroll").evaluate((el) => { el.scrollTop = el.scrollHeight; });
        await expect.poll(() => distance(page)).toBeLessThanOrEqual(4);
        await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");
      });
    }

    test("nested scrolling, consumed keys and zoom input leave following enabled", async ({ page }) => {
      const code = page.locator("pre").last();
      await code.evaluate((el) => { el.scrollTop = 60; });
      await code.hover();
      await page.mouse.wheel(0, -30);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "true");
      await code.evaluate((el) => { el.scrollTop = 0; });
      await page.mouse.wheel(0, -100);
      await page.getByRole("button", { name: "Row action", exact: true }).last().focus();
      await page.keyboard.press("Space");
      await page.keyboard.press("Home");
      await page.locator("input").last().focus();
      await page.keyboard.press("Home");
      await page.keyboard.press("End");
      await page.mouse.move(700, 300);
      await page.keyboard.down("Control");
      await page.mouse.wheel(0, -100);
      await page.keyboard.up("Control");
      await page.evaluate(() => window.chatScrollFixture.grow(75));
      await expect.poll(() => distance(page)).toBeLessThanOrEqual(4);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "true");
    });

    test("only a reader return to the bottom resumes following", async ({ page }) => {
      await page.mouse.move(700, 300);
      await page.mouse.wheel(0, -200);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");
      await page.locator("#chat-scroll").evaluate((el) => { el.scrollTop = el.scrollHeight; });
      await page.waitForTimeout(50);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");
      await page.locator("button").last().focus();
      await page.keyboard.press("End");
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "true");
      await page.evaluate(() => window.chatScrollFixture.grow(80));
      await expect.poll(() => distance(page)).toBeLessThanOrEqual(4);
      await page.mouse.move(700, 300);
      await page.mouse.wheel(0, -80);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "false");
      await page.mouse.wheel(0, 200);
      await expect(page.locator("#chat-scroll")).toHaveAttribute("data-following", "true");
    });
  });
}
