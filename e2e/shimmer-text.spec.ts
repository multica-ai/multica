import { readFileSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";
import { shimmerTextScript } from "../packages/ui/test/shimmer-text-bundle.mjs";

// No server required. Use the production markup/styles: jsdom cannot check
// counter-translated glyph alignment, text selection, or media fallbacks.
const css = readFileSync("packages/ui/styles/base.css", "utf8");

declare global {
  interface Window {
    shimmerFixture: { render(text: string, active: boolean): void };
  }
}

test.beforeEach(async ({ page }) => {
  await page.setContent(`<style>
    :root { --muted-foreground: #666; --foreground: #111; }
    ${css}
  </style><div id="mount"></div>`);
  await page.addScriptTag({ content: shimmerTextScript });
});

async function renderLabel(page: Page, text = "Working", active = true) {
  await page.evaluate(({ text, active }) => window.shimmerFixture.render(text, active), { text, active });
}

test("exposes and copies the label once, including localized and updated counts", async ({ page }) => {
  for (const text of ["Working", "2 agents working", "12 个智能体正在工作"]) {
    await renderLabel(page, text);
    expect(await page.evaluate(() => document.getAnimations().filter((a) => a.playState === "running").length)).toBe(2);
    expect(await page.locator("#fixture").ariaSnapshot()).toBe(`- text: ${text}`);
    const selected = await page.locator("#label").evaluate((label) => {
      const range = document.createRange();
      range.selectNodeContents(label);
      const selection = window.getSelection()!;
      selection.removeAllRanges();
      selection.addRange(range);
      return selection.toString();
    });
    expect(selected).toBe(text);
  }
});

test("keeps both copies aligned throughout the sweep and truncates long labels", async ({ page }) => {
  for (const text of ["Working", "12 个智能体正在工作", "Searching through a very long project name"]) {
    await renderLabel(page, text);
    // Include both sides of the repeating sweep's seam.
    for (const time of [0, 400, 1250, 2499, 2501]) {
      const geometry = await page.evaluate((time) => {
        for (const animation of document.getAnimations()) {
          animation.pause();
          animation.currentTime = time;
        }
        const label = document.querySelector<HTMLElement>("#label")!;
        const copy = label.querySelector<HTMLElement>(".shimmer-text-copy")!;
        const baseRect = label.getBoundingClientRect();
        const copyRect = copy.getBoundingClientRect();
        return {
          dx: Math.abs(baseRect.x - copyRect.x),
          dy: Math.abs(baseRect.y - copyRect.y),
          dw: Math.abs(baseRect.width - copyRect.width),
          width: baseRect.width,
          overflow: getComputedStyle(label).textOverflow,
          copyOverflow: getComputedStyle(copy).textOverflow,
          clipped: label.scrollWidth > label.clientWidth,
        };
      }, time);
      expect(geometry.dx).toBeLessThan(0.1);
      expect(geometry.dy).toBeLessThan(0.1);
      expect(geometry.dw).toBeLessThan(0.1);
      expect(geometry.width).toBeLessThanOrEqual(180);
      expect(geometry.overflow).toBe("ellipsis");
      expect(geometry.copyOverflow).toBe("ellipsis");
      if (text.startsWith("Searching")) expect(geometry.clipped).toBe(true);
    }
  }
});

test("remains readable without running animations when inactive or motion is reduced", async ({ page }) => {
  await renderLabel(page, "Queued", false);
  await expect(page.locator("#label")).toHaveText("Queued");
  expect(await page.evaluate(() => document.getAnimations().length)).toBe(0);

  await page.emulateMedia({ reducedMotion: "reduce" });
  await renderLabel(page);
  await expect(page.locator("#label")).toHaveCSS("color", "rgb(102, 102, 102)");
  await expect(page.locator(".shimmer-text-window")).toBeHidden();
  expect(await page.evaluate(() => document.getAnimations().length)).toBe(0);
});

test("uses readable system text and no highlight in forced colors", async ({ page }) => {
  await page.emulateMedia({ forcedColors: "active" });
  await renderLabel(page);
  await expect(page.locator(".shimmer-text-window")).toBeHidden();
  expect(await page.evaluate(() => document.getAnimations().length)).toBe(0);
  expect(await page.locator("#fixture").ariaSnapshot()).toBe("- text: Working");
  const colors = await page.locator("#label").evaluate((label) => {
    const probe = document.createElement("span");
    probe.style.color = "CanvasText";
    document.body.append(probe);
    const result = [getComputedStyle(label).color, getComputedStyle(probe).color];
    probe.remove();
    return result;
  });
  expect(colors[0]).toBe(colors[1]);
});
