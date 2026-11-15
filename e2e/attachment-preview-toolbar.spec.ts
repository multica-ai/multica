import { expect, test } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";

const apiBase = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;

for (const surface of ["standalone", "modal"] as const) {
  for (const width of [1440, 390]) {
    test(`HTML source toggle keeps its hit target in ${surface} at ${width}px`, async ({ page, baseURL }, testInfo) => {
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
        const before = (await source.boundingBox())!;
        const center = { x: before.x + before.width / 2, y: before.y + before.height / 2 };
        const previewShot = testInfo.outputPath("preview.png");
        await page.screenshot({ path: previewShot, animations: "disabled" });
        await testInfo.attach("preview", { path: previewShot, contentType: "image/png" });
        await page.mouse.click(center.x, center.y);
        await expect(source).toHaveAttribute("aria-pressed", "true");
        await expect(copy).toBeVisible();
        await expect(copy).toBeEnabled();
        const sourceShot = testInfo.outputPath("source.png");
        await page.screenshot({ path: sourceShot, animations: "disabled" });
        await testInfo.attach("source", { path: sourceShot, contentType: "image/png" });
        expect(await source.boundingBox()).toEqual(before);

        await page.mouse.click(center.x, center.y);
        await expect(source).toHaveAttribute("aria-pressed", "false");
        await expect(copy).toBeHidden();
        expect(await source.boundingBox()).toEqual(before);
      } finally {
        await api.cleanup();
      }
    });
  }
}
