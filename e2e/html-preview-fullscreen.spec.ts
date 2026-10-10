import { expect, test, type Locator } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

const html = `<style>html,body{margin:0;padding:0}</style>
<div style="height:480px"><input aria-label="Retained input" value="initial"></div>
${"<!-- long source -->\n".repeat(100)}`;
const content = `${"Paragraph before the preview.\n\n".repeat(60)}\`\`\`html\n${html}\n\`\`\``;

function readScroll(element: HTMLElement) {
  return { top: element.scrollTop, height: element.scrollHeight };
}

async function scrollContainer(block: Locator) {
  return block.evaluateHandle((element) => {
    for (let parent = element.parentElement; parent; parent = parent.parentElement) {
      if (
        /auto|scroll/.test(getComputedStyle(parent).overflowY) &&
        parent.scrollHeight > parent.clientHeight
      ) {
        return parent;
      }
    }
    throw new Error("Expected the issue's outer scroll container");
  });
}

test.describe("HTML preview fullscreen", () => {
  let api: TestApiClient;
  let workspaceSlug: string;

  test.beforeEach(async ({ page, baseURL }) => {
    // These fixtures create isolated local workspaces, never shared-site data.
    const local = (url: string) =>
      ["localhost", "127.0.0.1", "[::1]"].includes(new URL(url).hostname);
    test.skip(
      !local(baseURL!) || !local(process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080"),
      "Local fixtures only",
    );
    await page.setViewportSize({ width: 1440, height: 1080 });
    api = await createTestApi();
    workspaceSlug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api?.cleanup();
  });

  for (const location of ["description", "reply"] as const) {
    test(`${location} keeps its outer scroll position and loaded iframe`, async ({
      page,
    }, testInfo) => {
      const issue = await api.createIssue(`HTML preview ${location}`, {
        description: location === "description" ? content : "Reply preview regression",
      });
      const comment = location === "reply" ? await api.createComment(issue.id, content) : null;
      await page.goto(`/${workspaceSlug}/issues/${issue.identifier}`, {
        waitUntil: "domcontentloaded",
      });
      const outer = await scrollContainer(
        page.getByText("Paragraph before the preview.", { exact: true }).first(),
      );
      await outer.evaluate((element) => {
        element.scrollTop = element.scrollHeight - element.clientHeight;
      });
      const block = page.locator(
        location === "description"
          ? ".code-block-wrapper"
          : `[data-comment-content="${comment!.id}"] [data-dynamic-block="html"]`,
      );
      await block.scrollIntoViewIfNeeded();
      const iframe = block.locator("iframe");
      await expect(iframe).toBeVisible();
      await expect.poll(async () => (await iframe.boundingBox())?.height).toBe(480);
      const input = iframe.contentFrame().getByRole("textbox", { name: "Retained input" });
      await expect(input).toBeVisible();
      const frameElement = await iframe.elementHandle();
      const frame = (await frameElement!.contentFrame())!;
      await input.fill("Keep this document");
      const timeOrigin = await frame.evaluate(() => performance.timeOrigin);
      const scroller = await scrollContainer(block);
      await scroller.evaluate((element) => {
        element.scrollTop = element.scrollHeight - element.clientHeight;
      });
      const before = await scroller.evaluate(readScroll);
      expect(before.top).toBeGreaterThan(480);
      const blockHeight = (await block.boundingBox())!.height;
      const opener = block.getByRole("button", { name: "Fullscreen" });
      await block.hover();
      await testInfo.attach(`${location}-inline`, {
        body: await page.screenshot(),
        contentType: "image/png",
      });
      await expect(opener).toBeInViewport();

      for (let round = 0; round < 3; round++) {
        await opener.click();
        const dialog = page.getByRole("dialog");
        await expect(dialog).toBeVisible();
        if (round === 0)
          await testInfo.attach(`${location}-enlarged`, {
            body: await page.screenshot(),
            contentType: "image/png",
          });
        expect(
          await dialog.locator("iframe").evaluate((element, original) => element === original, frameElement),
        ).toBe(true);
        expect((await dialog.boundingBox())!.height).toBe(972);
        await expect.poll(async () => scroller.evaluate(readScroll)).toEqual(before);
        expect((await block.boundingBox())!.height).toBe(blockHeight);
        await expect(input).toHaveValue("Keep this document");
        expect(await frame.evaluate(() => performance.timeOrigin)).toBe(timeOrigin);
        if (round === 0) {
          await page.getByRole("button", { name: "Close", exact: true }).click();
        } else if (round === 1) {
          await page.mouse.click(4, 4);
        } else {
          await page.keyboard.press("Escape");
        }
        await expect(page.getByRole("dialog")).toHaveCount(0);
        await expect.poll(async () => scroller.evaluate(readScroll)).toEqual(before);
        expect((await block.boundingBox())!.height).toBe(blockHeight);
        expect(
          await iframe.evaluate((element, original) => element === original, frameElement),
        ).toBe(true);
        expect(await frame.evaluate(() => performance.timeOrigin)).toBe(timeOrigin);
        await expect(opener).toBeFocused();
      }
      await testInfo.attach(`${location}-restored`, {
        body: await page.screenshot(),
        contentType: "image/png",
      });
      await testInfo.attach(`${location}-geometry`, {
        body: Buffer.from(
          JSON.stringify({
            before,
            after: await scroller.evaluate(readScroll),
            rounds: 3,
            blockHeight,
            iframeHeight: 480,
            fullscreenHeight: 972,
            documentRetained: true,
            inputRetained: true,
          }),
        ),
        contentType: "application/json",
      });

      if (location === "description") {
        await block.getByRole("button", { name: "Show source" }).click();
        await opener.scrollIntoViewIfNeeded();
        const sourceHeight = (await block.boundingBox())!.height;
        const sourceScroll = await scroller.evaluate(readScroll);
        await opener.click();
        await expect(page.getByRole("dialog").locator("iframe")).toBeVisible();
        expect((await block.boundingBox())!.height).toBe(sourceHeight);
        expect(await scroller.evaluate(readScroll)).toEqual(sourceScroll);
        await page.keyboard.press("Escape");
        await expect(page.getByRole("dialog")).toHaveCount(0);
        expect((await block.boundingBox())!.height).toBe(sourceHeight);
        expect(await scroller.evaluate(readScroll)).toEqual(sourceScroll);
        expect(await frame.evaluate(() => performance.timeOrigin)).toBe(timeOrigin);
      }
    });
  }

  test("enlarging from expanded reply source preserves its panel and outer layout", async ({
    page,
  }, testInfo) => {
    const issue = await api.createIssue("Expanded HTML source regression");
    const comment = await api.createComment(issue.id, content);
    await page.goto(`/${workspaceSlug}/issues/${issue.identifier}`, {
      waitUntil: "domcontentloaded",
    });
    const outer = await scrollContainer(
      page.getByText("Paragraph before the preview.", { exact: true }).first(),
    );
    await outer.evaluate((element) => {
      element.scrollTop = element.scrollHeight - element.clientHeight;
    });
    const block = page.locator(
      `[data-comment-content="${comment.id}"] [data-dynamic-block="html"]`,
    );
    await block.scrollIntoViewIfNeeded();
    await block.getByRole("tab", { name: "Source" }).click();
    await block.getByRole("button", { name: "Show all" }).click();
    const source = block.locator("pre");
    const originalSource = await source.elementHandle();
    const beforeHeight = (await block.boundingBox())!.height;
    expect(beforeHeight).toBeGreaterThan(480);
    const scroller = await scrollContainer(block);
    const opener = block.getByRole("button", { name: "Fullscreen" });
    await opener.scrollIntoViewIfNeeded();
    const before = await scroller.evaluate(readScroll);
    await opener.click();
    await expect(page.getByRole("dialog").locator("iframe")).toBeVisible();
    expect(await originalSource!.evaluate((element) => element.isConnected)).toBe(true);
    expect((await block.boundingBox())!.height).toBe(beforeHeight);
    expect(await scroller.evaluate(readScroll)).toEqual(before);
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    expect(await source.evaluate((element, original) => element === original, originalSource)).toBe(
      true,
    );
    await expect(block.getByRole("tab", { name: "Source" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    await expect(block.getByRole("button", { name: "Show all" })).toHaveCount(0);
    expect((await block.boundingBox())!.height).toBe(beforeHeight);
    expect(await scroller.evaluate(readScroll)).toEqual(before);
    await expect(opener).toBeFocused();
    await testInfo.attach("source-restored", {
      body: await page.screenshot(),
      contentType: "image/png",
    });
    await testInfo.attach("source-geometry", {
      body: Buffer.from(
        JSON.stringify({
          before,
          after: await scroller.evaluate(readScroll),
          sourceHeight: beforeHeight,
          expandedSourceRetained: true,
          sourceElementRetained: true,
        }),
      ),
      contentType: "application/json",
    });
  });
});
