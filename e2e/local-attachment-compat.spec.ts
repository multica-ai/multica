import { test, expect } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { createTestApi, loginAsDefault } from "./helpers";

const apiBase = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;

test("local attachment historical image", async ({ page, baseURL }, testInfo) => {
  test.setTimeout(120000);
  const local = (url: string) => ["localhost", "127.0.0.1", "[::1]"].includes(new URL(url).hostname);
  test.skip(!local(baseURL!) || !local(apiBase) || !!process.env.S3_BUCKET, "Isolated LocalStorage fixtures only");
  const api = await createTestApi();
  try {
    const slug = await loginAsDefault(page);
    const issue = await api.createIssue("Local attachment compatibility");
    const form = new FormData();
    form.append("file", new Blob([await readFile("apps/web/public/images/landing-hero.webp")], { type: "image/webp" }), "workspace-preview.webp");
    form.append("issue_id", issue.id);
    const headers = { Authorization: `Bearer ${api.getToken()}`, "X-Workspace-Slug": slug };
    const uploaded = await fetch(`${apiBase}/api/upload-file`, { method: "POST", headers, body: form });
    expect(uploaded.status).toBe(200);
    const att = await uploaded.json();
    expect(att.url).toContain(`/api/attachments/${att.id}/download`);
    const ws = (await api.getWorkspaces()).find((w) => w.slug === slug)!;
    const raw = `/uploads/workspaces/${ws.id}/${att.id}.webp`;
    await api.updateIssue(issue.id, { description: `An image saved by an earlier client.\n\n![Workspace preview](${raw})` });
    await page.goto(`/${slug}/issues/${issue.identifier}`);
    await expect(page.getByText("An image saved by an earlier client.")).toBeVisible();
    const image = page.locator('img[alt="Workspace preview"]');
    await expect.poll(() => image.evaluate((el: HTMLImageElement) => el.naturalWidth)).toBeGreaterThan(0);
    expect(await image.getAttribute("src")).toMatch(/^(blob:|.*\/api\/attachments\/)/);
    await testInfo.attach("historical-image-restored", { body: await page.screenshot(), contentType: "image/png" });
  } finally { await api.cleanup(); }
});
