import { expect, test } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";

const apiBase = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;

for (const surface of ["standalone", "modal"] as const) {
  for (const width of [1440, 390, 320]) {
    test(`HTML toolbar keeps its controls reachable in ${surface} at ${width}px`, async ({ page, baseURL }, testInfo) => {
      test.setTimeout(120000);
      const local = (url: string) =>
        ["localhost", "127.0.0.1", "[::1]"].includes(new URL(url).hostname);
      test.skip(!local(baseURL!) || !local(apiBase), "Isolated local fixtures only");
      await page.setViewportSize({ width, height: 900 });
      const api = await createTestApi();
      try {
        const slug = await loginAsDefault(page);
        const issue = await api.createIssue("HTML attachment toolbar");
        const form = new FormData();
        form.append("file", new Blob([
          "<!doctype html><html><body><h1>Toolbar layout</h1><p>Switch between preview and source.</p></body></html>",
        ], { type: "text/html" }), "toolbar-layout.html");
        form.append("issue_id", issue.id);
        const uploaded = await fetch(`${apiBase}/api/upload-file`, {
          method: "POST",
          headers: { Authorization: `Bearer ${api.getToken()}`, "X-Workspace-Slug": slug },
          body: form,
        });
        expect(uploaded.status).toBe(200);
        const attachment = await uploaded.json();
        if (surface === "standalone") {
          await page.goto(`/${slug}/attachments/${attachment.id}/preview`);
        } else {
          await api.updateIssue(issue.id, {
            description: `!file[toolbar-layout.html](${attachment.markdown_url})`,
          });
          await page.goto(`/${slug}/issues/${issue.identifier}`);
          await page.getByRole("button", { name: "Preview", exact: true }).first().click();
          await expect(page.getByRole("dialog")).toBeVisible();
        }

        const source = page.getByRole("button", { name: "View source", exact: true });
        const copy = page.getByRole("button", { name: "Copy contents", exact: true });
        await expect(source).toBeVisible();
        await expect(copy).toBeHidden();
        const toolbar = source.locator("xpath=ancestor::header");
        const more = toolbar.getByRole("button", { name: "More actions", exact: true });
        const download = toolbar.getByRole("button", { name: "Download", exact: true });
        const close = toolbar.getByRole("button", { name: "Close", exact: true });
        const expectReachableControls = async () => {
          const bounds = (await toolbar.boundingBox())!;
          for (const control of [source, download, ...(surface === "modal" ? [close] : []), ...(await more.isVisible() ? [more] : []), ...(await copy.isVisible() ? [copy] : [])]) {
            await expect(control).toBeInViewport({ ratio: 1 });
            const box = (await control.boundingBox())!;
            expect(box.x).toBeGreaterThanOrEqual(bounds.x);
            expect(box.x + box.width).toBeLessThanOrEqual(bounds.x + bounds.width);
            if (bounds.width < 768) {
              expect(box.width).toBeGreaterThanOrEqual(44);
              expect(box.height).toBeGreaterThanOrEqual(44);
            }
          }
        };
        await expectReachableControls();
        if (width === 1440) {
          await expect(more).toBeHidden();
          // The preview's container, rather than the browser window, owns folding.
          await toolbar.evaluate((element) => { element.style.width = "390px"; });
          await expect(more).toBeVisible();
          await expectReachableControls();
          await toolbar.evaluate((element) => { element.style.width = ""; });
          await expect(more).toBeHidden();
        } else {
          await more.focus();
          await page.keyboard.press("ArrowDown");
          const menu = page.getByRole("menu");
          await expect(menu).toBeVisible();
          await expect(menu.getByRole("menuitemradio", { name: "Fit", exact: true })).toHaveAttribute("aria-checked", "true");
          await expect(menu.getByRole("menuitemcheckbox", { name: "Wrap lines", exact: true })).toBeDisabled();
          await expect(menu.getByRole("menuitemradio", { name: "Fit", exact: true })).toBeFocused();
          for (let step = 0; step < 3; step++) await page.keyboard.press("ArrowDown");
          await expect(menu.getByRole("menuitemradio", { name: "Phone", exact: true })).toBeFocused();
          await page.keyboard.press("Enter");
          await expect(menu).toBeHidden();
          await expect(more).toBeFocused();
          if (surface === "modal") {
            await page.keyboard.press("Tab");
            await expect(close).toBeFocused();
            await page.keyboard.press("Shift+Tab");
            await expect(more).toBeFocused();
          }
          await more.click();
          await expect(menu.getByRole("menuitemradio", { name: "Phone", exact: true })).toHaveAttribute("aria-checked", "true");
          await page.keyboard.press("Escape");
          await expect(menu).toBeHidden();
          await expect(more).toBeFocused();
          if (surface === "modal") await expect(page.getByRole("dialog")).toBeVisible();
        }
        const before = (await source.boundingBox())!;
        const center = { x: before.x + before.width / 2, y: before.y + before.height / 2 };
        const previewShot = testInfo.outputPath("preview.png");
        await page.screenshot({ path: previewShot, animations: "disabled" });
        await testInfo.attach("preview", { path: previewShot, contentType: "image/png" });
        await page.mouse.click(center.x, center.y);
        await expect(source).toHaveAttribute("aria-pressed", "true");
        await expect(copy).toBeVisible();
        await expect(copy).toBeEnabled();
        await expectReachableControls();
        const sourceShot = testInfo.outputPath("source.png");
        await page.screenshot({ path: sourceShot, animations: "disabled" });
        await testInfo.attach("source", { path: sourceShot, contentType: "image/png" });
        expect(await source.boundingBox()).toEqual(before);

        if (width < 768) {
          await more.click();
          const menu = page.getByRole("menu");
          await expect(menu.getByRole("menuitemradio", { name: "Phone", exact: true })).toBeDisabled();
          await expect(menu.getByRole("menuitem", { name: "Reload", exact: true })).toBeDisabled();
          const wrap = menu.getByRole("menuitemcheckbox", { name: "Wrap lines", exact: true });
          await expect(wrap).toBeEnabled();
          await expect(wrap).toHaveAttribute("aria-checked", "false");
          await wrap.click();
          await expect(wrap).toHaveAttribute("aria-checked", "true");
          if (surface === "modal" && width === 390) {
            const menuShot = testInfo.outputPath("menu.png");
            await page.screenshot({ path: menuShot, animations: "disabled" });
            await testInfo.attach("menu", { path: menuShot, contentType: "image/png" });
          }
          await page.keyboard.press("Escape");
          await expect(menu).toBeHidden();
          await expect(more).toBeFocused();
          if (surface === "modal") await expect(page.getByRole("dialog")).toBeVisible();
        }

        const [downloaded] = await Promise.all([
          page.waitForEvent("download"),
          download.click(),
        ]);
        expect(downloaded.suggestedFilename()).toBe("toolbar-layout.html");

        await page.mouse.click(center.x, center.y);
        await expect(source).toHaveAttribute("aria-pressed", "false");
        await expect(copy).toBeHidden();
        expect(await source.boundingBox()).toEqual(before);
        await expectReachableControls();
        if (surface === "modal") {
          await close.click();
          await expect(page.getByRole("dialog")).toBeHidden();
        }
      } finally {
        await api.cleanup();
      }
    });
  }
}

for (const kind of ["structured", "image"] as const) {
  test(`compact overflow retains ${kind} controls`, async ({ page, baseURL }) => {
    test.setTimeout(120000);
    const local = (url: string) =>
      ["localhost", "127.0.0.1", "[::1]"].includes(new URL(url).hostname);
    test.skip(!local(baseURL!) || !local(apiBase), "Isolated local fixtures only");
    await page.setViewportSize({ width: 320, height: 900 });
    const api = await createTestApi();
    try {
      const slug = await loginAsDefault(page);
      const issue = await api.createIssue(`Compact ${kind} toolbar`);
      const form = new FormData();
      const structured = kind === "structured";
      form.append("file", new Blob([
        structured
          ? '{"name":"toolbar","enabled":true}'
          : '<svg xmlns="http://www.w3.org/2000/svg" width="800" height="600"><rect width="800" height="600" fill="silver"/></svg>',
      ], { type: structured ? "application/json" : "image/svg+xml" }), structured ? "toolbar.json" : "toolbar.svg");
      form.append("issue_id", issue.id);
      const uploaded = await fetch(`${apiBase}/api/upload-file`, {
        method: "POST",
        headers: { Authorization: `Bearer ${api.getToken()}`, "X-Workspace-Slug": slug },
        body: form,
      });
      expect(uploaded.status).toBe(200);
      const attachment = await uploaded.json();
      await page.goto(`/${slug}/attachments/${attachment.id}/preview`);
      if (structured) {
        await expect(page.getByRole("button", { name: "Copy contents", exact: true })).toBeEnabled();
      } else {
        const image = page.getByRole("img", { name: "toolbar.svg", exact: true });
        await expect.poll(() => image.evaluate((element: HTMLImageElement) => element.naturalWidth)).toBe(800);
      }
      const more = page.getByRole("button", { name: "More actions", exact: true });
      await expect(more).toBeInViewport({ ratio: 1 });
      await more.focus();
      await page.keyboard.press("ArrowDown");
      const menu = page.getByRole("menu");
      if (structured) {
        await expect(menu.getByRole("menuitemradio", { name: "Tree", exact: true })).toHaveAttribute("aria-checked", "true");
        await expect(menu.getByRole("menuitemcheckbox", { name: "Wrap lines", exact: true })).toBeDisabled();
        await page.keyboard.press("ArrowDown");
        await expect(menu.getByRole("menuitemradio", { name: "Raw", exact: true })).toBeFocused();
        await page.keyboard.press("Enter");
        await expect(menu).toBeHidden();
        await expect(more).toBeFocused();
        await expect(page.locator("pre")).toContainText('{"name":"toolbar","enabled":true}');
        await more.click();
        await expect(menu.getByRole("menuitemradio", { name: "Raw", exact: true })).toHaveAttribute("aria-checked", "true");
        const wrap = menu.getByRole("menuitemcheckbox", { name: "Wrap lines", exact: true });
        await expect(wrap).toBeEnabled();
        await wrap.click();
        await expect(wrap).toHaveAttribute("aria-checked", "true");
      } else {
        await menu.getByRole("menuitem", { name: "Actual size", exact: true }).click();
        await expect(menu).toBeHidden();
        await expect(more).toBeFocused();
        await more.click();
        await expect(menu.getByText("100%", { exact: true })).toBeVisible();
        await menu.getByRole("menuitem", { name: "Zoom out", exact: true }).click();
        await expect(menu).toBeHidden();
        await expect(more).toBeFocused();
        await more.click();
        await expect(menu.getByText("100%", { exact: true })).toHaveCount(0);
        await expect(menu.getByRole("menuitem", { name: "Fit to view", exact: true })).toBeEnabled();
      }
      await page.keyboard.press("Escape");
      await expect(menu).toBeHidden();
      await expect(more).toBeFocused();
    } finally {
      await api.cleanup();
    }
  });
}
