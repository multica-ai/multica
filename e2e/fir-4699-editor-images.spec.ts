import {
  expect,
  test,
  type Browser,
  type Page,
  type TestInfo,
} from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

const PHONE = { width: 390, height: 844 };
const DESKTOP = { width: 1100, height: 900 };
const TEST_IMAGE = {
  name: "fir-4699.svg",
  mimeType: "image/svg+xml",
  buffer: Buffer.from(
    `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="180" viewBox="0 0 320 180">
      <rect width="320" height="180" fill="#635bff" />
      <circle cx="70" cy="70" r="35" fill="#ffffff" />
      <text x="32" y="150" fill="#ffffff" font-family="sans-serif" font-size="24">FIR-4699</text>
    </svg>`,
  ),
};

// Local worktrees serve the UI and uploaded files from separate HTTP ports.
// Production uses HTTPS, which the app CSP already permits for image sources.
test.use({ viewport: PHONE, hasTouch: true, isMobile: true, bypassCSP: true });

async function expectImageLoaded(image: ReturnType<Page["locator"]>) {
  await expect
    .poll(() =>
      image.evaluate(
        (element: HTMLImageElement) =>
          element.complete &&
          element.naturalWidth > 0 &&
          element.naturalHeight > 0,
      ),
    )
    .toBe(true);
}

async function simulateOpenPhoneKeyboard(page: Page) {
  const keyboardHeight = 336;
  await page.evaluate((height) => {
    const viewport = window.visualViewport;
    if (!viewport) throw new Error("visualViewport missing");
    Object.defineProperty(viewport, "height", {
      configurable: true,
      get: () => window.innerHeight - height,
    });
    viewport.dispatchEvent(new Event("resize"));
  }, keyboardHeight);

  const toolbar = page.getByRole("toolbar", { name: "Formatting toolbar" });
  await expect(toolbar).toBeVisible();
  await expect
    .poll(() =>
      toolbar.evaluate(
        (row) => window.innerHeight - row.getBoundingClientRect().bottom,
      ),
    )
    .toBeGreaterThanOrEqual(keyboardHeight - 1);
}

async function dismissPhoneKeyboard(page: Page) {
  await page.evaluate(() => {
    const viewport = window.visualViewport;
    if (!viewport) throw new Error("visualViewport missing");
    Object.defineProperty(viewport, "height", {
      configurable: true,
      get: () => window.innerHeight,
    });
    (document.activeElement as HTMLElement | null)?.blur();
    viewport.dispatchEvent(new Event("resize"));
  });
  await expect(
    page.getByRole("toolbar", { name: "Formatting toolbar" }),
  ).toHaveCount(0);
}

async function insertFromActions(
  page: Page,
  triggerLabel: "Note actions" | "Document actions",
) {
  const chooser = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: triggerLabel }).tap();
  await page.getByRole("menuitem", { name: "Insert image" }).tap();
  await (await chooser).setFiles(TEST_IMAGE);

  const figure = page.locator(".rich-text-editor .image-figure").first();
  await expect(figure).toBeVisible({ timeout: 15000 });
  // The upload is done once the temporary blob: preview is swapped for the
  // stored attachment URL. That URL is relative (`/uploads/...`) on local
  // storage and absolute on remote storage — both are valid, so assert the
  // swap happened rather than the URL shape.
  await expect
    .poll(() => figure.locator("img").getAttribute("src"))
    .not.toMatch(/^blob:/);
  await expectImageLoaded(figure.locator("img"));
  return figure;
}

async function longPress(page: Page, target: ReturnType<Page["locator"]>) {
  await target.evaluate((element) => {
    const box = element.getBoundingClientRect();
    const touch = new Touch({
      identifier: 1,
      target: element,
      clientX: box.left + box.width / 2,
      clientY: box.top + box.height / 2,
    });
    element.dispatchEvent(
      new TouchEvent("touchstart", {
        bubbles: true,
        cancelable: true,
        touches: [touch],
        targetTouches: [touch],
        changedTouches: [touch],
      }),
    );
  });

  // The image menu intentionally opens after Base UI's native 500 ms hold.
  await page.waitForTimeout(550);
  await expect(page.getByRole("menu")).toBeVisible();
  await target.evaluate((element) => {
    element.dispatchEvent(
      new TouchEvent("touchend", {
        bubbles: true,
        cancelable: true,
        touches: [],
        targetTouches: [],
        changedTouches: [],
      }),
    );
  });
}

async function moveThroughBothPlacements(page: Page) {
  await dismissPhoneKeyboard(page);
  const figure = page.locator(".rich-text-editor .image-figure").first();
  await longPress(page, figure);
  await page.getByRole("menuitem", { name: "Move to bottom" }).click();
  await dismissPhoneKeyboard(page);

  const tray = page.getByRole("list", { name: "Attached images" });
  await expect(tray).toBeVisible();
  await expect(page.locator(".rich-text-editor .image-figure")).toHaveCount(0);
  await assertNoHorizontalOverflow(page, tray);

  const placeInText = tray.getByRole("button", {
    name: "Place image 1 in text",
  });
  await placeInText.evaluate((button) =>
    button.scrollIntoView({ block: "center", inline: "center" }),
  );
  await placeInText.tap();
  await expect(figure).toBeVisible();

  await longPress(page, figure);
  await page.getByRole("menuitem", { name: "Full width" }).click();
  await expect.poll(() => figure.evaluate((el) => el.style.width)).toBe("100%");
  await assertNoHorizontalOverflow(page, figure);
}

async function assertNoHorizontalOverflow(
  page: Page,
  target: ReturnType<Page["locator"]>,
) {
  const metrics = await target.evaluate((element) => {
    const box = element.getBoundingClientRect();
    return {
      viewportWidth: window.innerWidth,
      pageOverflow:
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
      left: box.left,
      right: box.right,
    };
  });
  expect(metrics.viewportWidth).toBe(PHONE.width);
  expect(metrics.pageOverflow).toBeLessThanOrEqual(1);
  expect(metrics.left).toBeGreaterThanOrEqual(-1);
  expect(metrics.right).toBeLessThanOrEqual(metrics.viewportWidth + 1);
}

async function openAsViewer(
  browser: Browser,
  ownerPage: Page,
  api: TestApiClient,
  path: string,
  identity: string,
) {
  const viewer = await api.loginSecondaryUser(identity, "FIR-4699 Viewer");
  const context = await browser.newContext({
    baseURL: new URL(ownerPage.url()).origin,
    viewport: PHONE,
    hasTouch: true,
    isMobile: true,
    bypassCSP: true,
  });
  await context.addInitScript((token) => {
    localStorage.setItem("multica_token", token);
  }, viewer.token);
  const page = await context.newPage();
  await page.goto(path, { waitUntil: "domcontentloaded" });
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem("multica_token")))
    .toBe(viewer.token);
  await expect(
    page.locator('.rich-text-editor[contenteditable="true"]'),
  ).toHaveCount(0);
  return page;
}

async function capturePhoneEvidence(
  page: Page,
  testInfo: TestInfo,
  name: string,
) {
  const path = testInfo.outputPath(`${name}-390-readonly.png`);
  await page.screenshot({ path, fullPage: true });
  await testInfo.attach(`${name} at 390 px`, {
    path,
    contentType: "image/png",
  });
}

async function testImageDataTransfer(page: Page) {
  return page.evaluateHandle(
    ({ name, mimeType, bytes }) => {
      const transfer = new DataTransfer();
      transfer.items.add(
        new File([new Uint8Array(bytes)], name, { type: mimeType }),
      );
      return transfer;
    },
    {
      name: TEST_IMAGE.name,
      mimeType: TEST_IMAGE.mimeType,
      bytes: Array.from(TEST_IMAGE.buffer),
    },
  );
}

async function captureDesktopEvidence(
  page: Page,
  testInfo: TestInfo,
  name: string,
) {
  const path = testInfo.outputPath(`${name}.png`);
  await page.screenshot({ path, fullPage: true });
  await testInfo.attach(name, { path, contentType: "image/png" });
}

test.describe("FIR-4699 — Notes and Documents image flows", () => {
  let api: TestApiClient;
  let workspaceSlug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_editor_images", true);
    await api.setWorkspaceFeatureFlag("cerebro_editor_toolbar", true);
    workspaceSlug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.setWorkspaceFeatureFlag("cerebro_editor_images", false);
    await api.setWorkspaceFeatureFlag("cerebro_editor_toolbar", false);
    await api.cleanup();
  });

  test("Note image stays reachable above the phone keyboard and matches read-only after reload", async ({
    browser,
    page,
  }, testInfo) => {
    const note = await api.createSharedNote(
      `FIR-4699 note ${Date.now()}`,
      "Image follows this sentence.",
      "viewer",
    );
    const path = `/${workspaceSlug}/notes/${note.id}`;
    await page.goto(path);

    const editor = page
      .locator('.rich-text-editor[contenteditable="true"]')
      .first();
    await expect(editor).toBeVisible({ timeout: 15000 });
    await editor.getByText("Image follows this sentence.").tap();
    await simulateOpenPhoneKeyboard(page);

    await insertFromActions(page, "Note actions");
    await expect
      .poll(() => api.getLatestAttachmentArtifactId(TEST_IMAGE.name))
      .toBe(note.id);
    await moveThroughBothPlacements(page);
    await expect
      .poll(() => api.getArtifactBody(note.id))
      .toContain('data-width-pct="100"');

    const readonlyPage = await openAsViewer(
      browser,
      page,
      api,
      path,
      `fir-4699-note-viewer-${Date.now()}@multica.ai`,
    );
    const readonlyFigure = readonlyPage
      .locator(".rich-text-editor .image-figure")
      .first();
    await expect(readonlyFigure).toBeVisible({ timeout: 15000 });
    await expectImageLoaded(readonlyFigure.locator("img"));
    await expect
      .poll(() => readonlyFigure.evaluate((el) => el.style.width))
      .toBe("100%");
    await assertNoHorizontalOverflow(readonlyPage, readonlyFigure);
    await capturePhoneEvidence(readonlyPage, testInfo, "fir-4699-note");
    await readonlyPage.context().close();
  });

  test("Document image survives both placements and matches read-only after reload", async ({
    browser,
    page,
  }, testInfo) => {
    const document = await api.createAgentDocument(
      `FIR-4699 document ${Date.now()}`,
      "Image follows this sentence.",
    );
    const path = `/${workspaceSlug}/documents/${document.id}`;
    await page.goto(path);

    const editor = page
      .locator('.rich-text-editor[contenteditable="true"]')
      .first();
    await expect(editor).toBeVisible({ timeout: 15000 });
    await editor.getByText("Image follows this sentence.").tap();
    await insertFromActions(page, "Document actions");
    await expect
      .poll(() => api.getLatestAttachmentArtifactId(TEST_IMAGE.name))
      .toBe(document.id);
    await moveThroughBothPlacements(page);
    await expect
      .poll(() => api.getArtifactBody(document.id))
      .toContain('data-width-pct="100"');

    const readonlyPage = await openAsViewer(
      browser,
      page,
      api,
      path,
      `fir-4699-document-viewer-${Date.now()}@multica.ai`,
    );
    const readonlyFigure = readonlyPage
      .locator(".rich-text-editor .image-figure")
      .first();
    await expect(readonlyFigure).toBeVisible({ timeout: 15000 });
    await expectImageLoaded(readonlyFigure.locator("img"));
    await expect
      .poll(() => readonlyFigure.evaluate((el) => el.style.width))
      .toBe("100%");
    await assertNoHorizontalOverflow(readonlyPage, readonlyFigure);
    await capturePhoneEvidence(readonlyPage, testInfo, "fir-4699-document");
    await readonlyPage.context().close();
  });
});

test.describe("FIR-4699 — Document drop and desktop layout", () => {
  test.use({
    viewport: DESKTOP,
    hasTouch: false,
    isMobile: false,
    bypassCSP: true,
  });

  let api: TestApiClient;
  let workspaceSlug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_editor_images", true);
    await api.setWorkspaceFeatureFlag("cerebro_editor_toolbar", true);
    await api.setWorkspaceFeatureFlag("cerebro_note_comments", true);
    workspaceSlug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.setWorkspaceFeatureFlag("cerebro_editor_images", false);
    await api.setWorkspaceFeatureFlag("cerebro_editor_toolbar", false);
    await api.setWorkspaceFeatureFlag("cerebro_note_comments", false);
    await api.cleanup();
  });

  test("real document drop shows its landing line and round-trips through tray, resize, comments, and dark mode", async ({
    page,
  }, testInfo) => {
    const document = await api.createAgentDocument(
      `FIR-4699 desktop drop ${Date.now()}`,
      "Drop above this line.\n\nDrop below this line.",
    );
    const path = `/${workspaceSlug}/documents/${document.id}`;
    await page.goto(path);

    const editor = page
      .locator('.rich-text-editor[contenteditable="true"]')
      .first();
    await expect(editor).toBeVisible({ timeout: 15000 });
    const dropTarget = editor.locator("p").last();
    const targetBox = await dropTarget.boundingBox();
    expect(targetBox).not.toBeNull();
    if (!targetBox) throw new Error("Document drop target has no layout box");

    const dataTransfer = await testImageDataTransfer(page);
    const pointer = {
      clientX: targetBox.x + targetBox.width / 2,
      clientY: targetBox.y + targetBox.height - 1,
      dataTransfer,
    };
    await dropTarget.dispatchEvent("dragenter", pointer);
    await dropTarget.dispatchEvent("dragover", pointer);

    const landingLine = page.getByTestId("image-drop-landing-line");
    await expect(landingLine).toBeVisible();
    const lineBox = await landingLine.boundingBox();
    expect(lineBox).not.toBeNull();
    expect(
      Math.abs((lineBox?.y ?? 0) - (targetBox.y + targetBox.height)),
    ).toBeLessThanOrEqual(3);
    await captureDesktopEvidence(
      page,
      testInfo,
      "fir-4699-document-drop-landing-line",
    );

    await dropTarget.dispatchEvent("drop", pointer);
    await dataTransfer.dispose();
    await expect(landingLine).toHaveCount(0);

    let figure = page.locator(".rich-text-editor .image-figure").first();
    await expect(figure).toBeVisible({ timeout: 15000 });
    await expectImageLoaded(figure.locator("img"));
    await expect
      .poll(() => api.getLatestAttachmentArtifactId(TEST_IMAGE.name))
      .toBe(document.id);
    await expect
      .poll(() => api.getArtifactBody(document.id))
      .toContain('data-placement="inline"');

    await figure.click();
    await figure.getByTitle("Move to bottom").click({ force: true });
    const tray = page.getByRole("list", { name: "Attached images" });
    await expect(tray).toBeVisible();
    await expect(page.locator(".rich-text-editor .image-figure")).toHaveCount(0);
    await expect
      .poll(() => api.getArtifactBody(document.id))
      .toContain("![image 1]");

    await page.reload();
    await expect(tray).toBeVisible({ timeout: 15000 });
    await expect(page.locator(".rich-text-editor .image-figure")).toHaveCount(0);
    await tray
      .getByRole("button", { name: "Place image 1 in text" })
      .click({ force: true });
    figure = page.locator(".rich-text-editor .image-figure").first();
    await expect(figure).toBeVisible();
    await expect
      .poll(() => api.getArtifactBody(document.id))
      .toContain('data-placement="inline"');

    await figure.click();
    const resizeHandle = figure.locator(".image-resize-handle-se");
    await expect(resizeHandle).toBeVisible();
    const handleBox = await resizeHandle.boundingBox();
    const figureBox = await figure.boundingBox();
    expect(handleBox).not.toBeNull();
    expect(figureBox).not.toBeNull();
    if (!handleBox || !figureBox) {
      throw new Error("Desktop image resize controls have no layout box");
    }
    const columnWidth = await editor.evaluate((element) => element.clientWidth);
    await page.mouse.move(
      handleBox.x + handleBox.width / 2,
      handleBox.y + handleBox.height / 2,
    );
    await page.mouse.down();
    await page.mouse.move(
      figureBox.x + columnWidth / 2,
      handleBox.y + handleBox.height / 2,
    );
    await page.mouse.up();
    await expect
      .poll(() => figure.evaluate((element) => element.style.width))
      .toBe("50%");
    await expect
      .poll(() => api.getArtifactBody(document.id))
      .toContain('data-width-pct="50"');

    await page.evaluate(() => localStorage.setItem("theme", "dark"));
    await page.reload();
    await expect(page.locator("html")).toHaveClass(/dark/);
    figure = page.locator(".rich-text-editor .image-figure").first();
    await expect(figure).toBeVisible({ timeout: 15000 });
    await expect
      .poll(() => figure.evaluate((element) => element.style.width))
      .toBe("50%");

    const closed = await figure.evaluate((element) => ({
      figureWidth: element.getBoundingClientRect().width,
      editorWidth:
        element.closest(".rich-text-editor")?.getBoundingClientRect().width ?? 0,
    }));
    await page.getByRole("button", { name: "Document actions" }).click();
    await page.getByRole("menuitem", { name: "Comments" }).click();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("menuitem", { name: "Comments" })).toHaveCount(0);
    await expect(page.getByText("Comments", { exact: true }).last()).toBeVisible();
    await expect
      .poll(() =>
        figure.evaluate(
          (element) =>
            element.closest(".rich-text-editor")?.getBoundingClientRect()
              .width ?? 0,
        ),
      )
      .toBeLessThan(closed.editorWidth);
    const reflowed = await figure.evaluate((element) => ({
      figureWidth: element.getBoundingClientRect().width,
      editorWidth:
        element.closest(".rich-text-editor")?.getBoundingClientRect().width ?? 0,
    }));
    expect(reflowed.editorWidth).toBeLessThan(closed.editorWidth);
    expect(reflowed.figureWidth).toBeLessThan(closed.figureWidth);
    expect(reflowed.figureWidth / reflowed.editorWidth).toBeCloseTo(0.5, 1);

    await captureDesktopEvidence(
      page,
      testInfo,
      "fir-4699-document-dark-comments-50-percent",
    );
  });
});
