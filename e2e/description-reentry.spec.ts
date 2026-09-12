import { expect, test } from "@playwright/test";
import { createTestApi, loginAsDefault, reloadAppPage } from "./helpers";
import type { TestApiClient } from "./fixtures";

type Frame = {
  issue: string;
  text: string;
  height: number;
  imageHeight: number;
  imageWidth: number;
  scroll: number;
  anchorTop: number | null;
  pageY: number;
  readonly: boolean;
  ready: boolean;
  imageLoaded: boolean;
};

declare global {
  interface Window {
    descriptionFrames: Frame[];
    recordDescription: boolean;
    releaseEditorCreates: () => void;
  }
}

const body = [
  "# Reentry A",
  "![Reentry image](/e2e-description.svg)",
  ...Array.from({ length: 100 }, (_, i) => `## Section ${i}\n\nParagraph ${i} of the cached description.\n\n- First item\n- Second item\n\n\`\`\`ts\nconst section = ${i};\n\`\`\``),
  "End of reentry A.",
].join("\n\n");

const anchorText = "Section 50";

function expectStableDescriptionFrames(
  frames: Frame[],
  {
    issueId,
    expectedText,
    absentText,
    expectedScroll,
    expectedAnchorTop,
  }: {
    issueId: string;
    expectedText: string;
    absentText?: string;
    expectedScroll: number;
    expectedAnchorTop?: number;
  },
) {
  expect(frames.length).toBeGreaterThan(0);
  expect(frames[0]!.text).toContain(expectedText);

  for (const frame of frames) {
    expect(frame.issue).toContain(issueId);
    expect(frame.text).toContain(expectedText);
    if (absentText) expect(frame.text).not.toContain(absentText);
    expect(frame.readonly).toBe(false);
    expect(Math.abs(frame.scroll - expectedScroll)).toBeLessThanOrEqual(1);
  }

  // Restored task descriptions keep native image sizing. Markdown images have
  // no dimensions before their resource decodes, so browser image loading is
  // intentionally measured separately from editor readiness and re-entry.
  const settledFrames = frames.filter((frame) => frame.imageLoaded);
  expect(settledFrames.length).toBeGreaterThanOrEqual(3);
  const firstSettled = settledFrames[0]!;

  for (const frame of settledFrames) {
    expect(frame.height).toBeCloseTo(firstSettled.height, 1);
    expect(frame.imageHeight).toBeCloseTo(firstSettled.imageHeight, 1);
    expect(frame.imageWidth).toBeCloseTo(firstSettled.imageWidth, 1);
    expect(frame.pageY).toBeCloseTo(firstSettled.pageY, 1);
    if (expectedAnchorTop !== undefined) {
      expect(frame.anchorTop).not.toBeNull();
      expect(Math.abs((frame.anchorTop ?? Infinity) - expectedAnchorTop)).toBeLessThanOrEqual(2);
    }
  }

  const readyImage = settledFrames.find(
    (frame) => frame.ready && frame.imageLoaded && frame.imageWidth > 0,
  );
  expect(readyImage).toBeDefined();
  expect(readyImage!.imageHeight / readyImage!.imageWidth).toBeCloseTo(0.5, 2);
}

test.describe("#8083 description initialization", () => {
  test.describe.configure({ timeout: 120000 });
  let api: TestApiClient;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await page.route("**/e2e-description.svg", route => route.fulfill({
      contentType: "image/svg+xml",
      headers: { "cache-control": "public, max-age=3600" },
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="640" height="320"><rect width="640" height="320" fill="teal"/></svg>',
    }));
    await page.addInitScript(() => {
      window.descriptionFrames = [];
      window.recordDescription = false;
      function sample() {
        const host = document.querySelector('[data-testid="issue-description"]');
        const editor = host?.querySelector<HTMLElement>(".ProseMirror");
        const image = host?.querySelector("img");
        const anchor = Array.from(editor?.querySelectorAll<HTMLElement>("h2") ?? [])
          .find((heading) => heading.textContent === "Section 50");
        if (window.recordDescription && host) {
          const scroll = host.closest<HTMLElement>("[data-tab-scroll-root]");
          const scrollBounds = scroll?.getBoundingClientRect();
          window.descriptionFrames.push({
            issue: scroll?.dataset.tabScrollRoot ?? "",
            text: editor?.textContent ?? "",
            height: host.getBoundingClientRect().height,
            imageHeight: image?.getBoundingClientRect().height ?? 0,
            imageWidth: image?.getBoundingClientRect().width ?? 0,
            scroll: scroll?.scrollTop ?? 0,
            anchorTop: anchor && scrollBounds
              ? anchor.getBoundingClientRect().top - scrollBounds.top
              : null,
            pageY: window.scrollY,
            readonly: !!host.querySelector("[data-rich-content]"),
            ready: !!(editor as HTMLElement & { editor?: { isInitialized: boolean } } | null)?.editor?.isInitialized,
            imageLoaded: !!image?.complete && image.naturalWidth > 0,
          });
        }
        requestAnimationFrame(sample);
      }
      requestAnimationFrame(sample);
    });
  });

  test.afterEach(async () => { await api?.cleanup(); });

  test("cached detail re-entry keeps its content, geometry, and restored scroll stable", async ({ page }, testInfo) => {
    const a = await api.createIssue(`E2E Reentry A ${Date.now()}`, { description: body });
    const b = await api.createIssue(`E2E Reentry B ${Date.now()}`, {
      description: "Reentry B distinct description.",
    });
    const slug = await loginAsDefault(page);
    const description = page.getByTestId("issue-description");
    const list = page.locator(`a[href="/${slug}/issues"]`).first();
    const open = async (id: string) => {
      await page.locator(`a[href$="/issues/${id}"]`).first().click();
      await expect(description.locator(".ProseMirror")).toBeVisible({ timeout: 30000 });
    };
    const leaveDetail = async () => {
      await list.click();
      await expect(description).toHaveCount(0);
    };
    const captureReentry = async (id: string) => {
      await expect(description).toHaveCount(0);
      await page.evaluate(() => {
        window.descriptionFrames = [];
        window.recordDescription = true;
      });
      await open(id);
      await page.waitForFunction(
        () => window.descriptionFrames.filter((frame) => frame.imageLoaded).length >= 3
          && window.descriptionFrames.some((frame) => frame.ready && frame.imageLoaded),
      );
      return page.evaluate(() => {
        window.recordDescription = false;
        return window.descriptionFrames;
      });
    };

    // Warm cached issue data and the original 2:1 image presentation before
    // sampling a zero-scroll re-entry.
    await open(a.id);
    await expect(description.locator("img")).toBeVisible();
    await description.locator("img").evaluate((img: HTMLImageElement) => img.decode());
    await leaveDetail();
    await open(b.id);
    await leaveDetail();

    const zeroScrollFrames = await captureReentry(a.id);
    expectStableDescriptionFrames(zeroScrollFrames, {
      issueId: a.id,
      expectedText: "Reentry A",
      absentText: "Reentry B distinct description.",
      expectedScroll: 0,
    });

    const savedPosition = await page.waitForFunction(
      ({ scrollKey, anchorLabel }) => {
        const scroll = document.querySelector<HTMLElement>(`[data-tab-scroll-root="${scrollKey}"]`);
        const anchor = Array.from(scroll?.querySelectorAll<HTMLElement>(".ProseMirror h2") ?? [])
          .find((heading) => heading.textContent?.trim() === anchorLabel);
        if (!scroll || !anchor) return null;

        const scrollBounds = scroll.getBoundingClientRect();
        const anchorBounds = anchor.getBoundingClientRect();
        scroll.scrollTop += anchorBounds.top - scrollBounds.top - scroll.clientHeight / 3;
        scroll.dispatchEvent(new Event("scroll", { bubbles: true }));
        return {
          scroll: scroll.scrollTop,
          anchorTop: anchor.getBoundingClientRect().top - scroll.getBoundingClientRect().top,
        };
      },
      { scrollKey: `main:${a.id}`, anchorLabel: anchorText },
    ).then((handle) => handle.jsonValue<{ scroll: number; anchorTop: number }>());
    expect(savedPosition.scroll).toBeGreaterThan(0);

    await leaveDetail();
    const sameIssueFrames = await captureReentry(a.id);
    expectStableDescriptionFrames(sameIssueFrames, {
      issueId: a.id,
      expectedText: "Reentry A",
      absentText: "Reentry B distinct description.",
      expectedScroll: savedPosition.scroll,
      expectedAnchorTop: savedPosition.anchorTop,
    });

    // A → list → B → list → A must retain A's scroll memento and never show
    // B's cached document during A's first visible frame or readiness path.
    await leaveDetail();
    await open(b.id);
    await expect(description).toContainText("Reentry B distinct description.");
    await leaveDetail();
    const crossIssueFrames = await captureReentry(a.id);
    expectStableDescriptionFrames(crossIssueFrames, {
      issueId: a.id,
      expectedText: "Reentry A",
      absentText: "Reentry B distinct description.",
      expectedScroll: savedPosition.scroll,
      expectedAnchorTop: savedPosition.anchorTop,
    });

    await testInfo.attach("description-reentry-frames", {
      body: JSON.stringify({ zeroScrollFrames, sameIssueFrames, crossIssueFrames }, null, 2),
      contentType: "application/json",
    });
  });

  test("first edit and file drop survive startup and save on immediate navigation", async ({ page }) => {
    const issue = await api.createIssue(`E2E Startup ${Date.now()}`, { description: body });
    // Hold only Tiptap's existing create task until both inputs arrive. A
    // fixed delay would race slower browsers or the first route compilation.
    await page.addInitScript(() => {
      const schedule = window.setTimeout;
      const pending: (() => void)[] = [];
      window.releaseEditorCreates = () => { pending.splice(0).forEach(release => release()); };
      window.setTimeout = ((callback: TimerHandler, delay?: number, ...args: unknown[]) => {
        if (typeof callback === "function" && /\.emit\(["']create["']/.test(callback.toString())) {
          const timer = schedule(callback, 60000, ...args);
          pending.push(() => { clearTimeout(timer); callback(...args); });
          return timer;
        }
        return schedule(callback, delay, ...args);
      }) as typeof window.setTimeout;
    });
    const slug = await loginAsDefault(page);
    await reloadAppPage(page);
    await page.locator(`a[href$="/issues/${issue.id}"]`).first().click();
    const host = page.getByTestId("issue-description");
    const editor = host.locator(".ProseMirror");
    await expect(editor).toBeVisible();
    expect(await editor.evaluate(el => (el as HTMLElement & { editor: { isInitialized: boolean } }).editor.isInitialized)).toBe(false);
    await editor.locator("h1").click();
    await page.keyboard.insertText("FIRSTEDIT ");
    await host.evaluate(el => {
      const data = new DataTransfer();
      data.items.add(new File(["first drop"], "first-drop.txt", { type: "text/plain" }));
      el.dispatchEvent(new DragEvent("drop", { bubbles: true, cancelable: true, dataTransfer: data }));
    });
    expect(await editor.evaluate(el => (el as HTMLElement & { editor: { isInitialized: boolean } }).editor.isInitialized)).toBe(false);
    await page.evaluate(() => window.releaseEditorCreates());
    await page.waitForFunction(() => (document.querySelector('[data-testid="issue-description"] .ProseMirror') as HTMLElement & { editor: { isInitialized: boolean } })?.editor.isInitialized);
    await expect(editor).toContainText("FIRSTEDIT");
    await expect(editor).toContainText("first-drop.txt");
    await expect(editor.locator('[data-uploading]')).toHaveCount(0);
    await page.locator(`a[href="/${slug}/issues"]`).first().click();
    await page.locator(`a[href$="/issues/${issue.id}"]`).first().click();
    await expect(page.getByTestId("issue-description")).toContainText("FIRSTEDIT");
    await expect(page.getByTestId("issue-description")).toContainText("first-drop.txt");
  });
});
