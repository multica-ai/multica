import { test, expect, type Frame } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";

const apiBase = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
// The full browser includes the native PDF viewer; headless-shell does not.
test.use({ channel: "chromium" });

function twoPagePDF(): Buffer {
  const stream = (text: string) => {
    const body = `BT /F1 24 Tf 72 700 Td (${text}) Tj ET`;
    return `<< /Length ${body.length} >>\nstream\n${body}\nendstream`;
  };
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 7 0 R >> >> /Contents 4 0 R >>",
    stream("Attachment preview - page one"),
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 7 0 R >> >> /Contents 6 0 R >>",
    stream("Keep reading - page two"),
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
  ];
  let pdf = "%PDF-1.4\n";
  const offsets = [0];
  objects.forEach((object, index) => {
    offsets.push(Buffer.byteLength(pdf));
    pdf += `${index + 1} 0 obj\n${object}\nendobj\n`;
  });
  const xref = Buffer.byteLength(pdf);
  pdf += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
  pdf += offsets.slice(1).map((offset) => `${String(offset).padStart(10, "0")} 00000 n \n`).join("");
  pdf += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(pdf);
}

test("cross-origin local PDF keeps its reading position past capability TTL", async ({ page, baseURL }, testInfo) => {
  test.setTimeout(120000);
  const local = (url: string) => ["localhost", "127.0.0.1", "[::1]"].includes(new URL(url).hostname);
  test.skip(!local(baseURL!) || !local(apiBase) || !!process.env.S3_BUCKET, "Isolated LocalStorage fixtures only");
  const api = await createTestApi();
  try {
    const slug = await loginAsDefault(page);
    const issue = await api.createIssue("Local PDF reading position");
    const form = new FormData();
    form.append("file", new Blob([twoPagePDF()], { type: "application/pdf" }), "reading.pdf");
    form.append("issue_id", issue.id);
    const uploaded = await fetch(`${apiBase}/api/upload-file`, {
      method: "POST", headers: { Authorization: `Bearer ${api.getToken()}`, "X-Workspace-Slug": slug }, body: form,
    });
    expect(uploaded.status).toBe(200);
    const att = await uploaded.json();
    await page.goto(`/${slug}/attachments/${att.id}/preview`);
    const frame = page.locator('iframe[title="reading.pdf"]');
    await expect(frame).toHaveAttribute("src", /^blob:/);
    let viewerFrame: Frame | undefined;
    await expect.poll(async () => {
      for (const child of page.frames()) {
        if (await child.locator("pdf-viewer").count()) { viewerFrame = child; return true; }
      }
      return false;
    }).toBe(true);
    const pageNumber = viewerFrame!.getByRole("textbox", { name: "Page number" });
    await pageNumber.fill("2");
    await pageNumber.press("Enter");
    await expect(pageNumber).toHaveValue("2");
    const source = await frame.getAttribute("src");
    const timeOrigin = await viewerFrame!.evaluate(() => performance.timeOrigin);
    const before = await page.screenshot({ path: testInfo.outputPath("pdf-page-two-before.png") });
    await testInfo.attach("pdf-page-two-before", { body: before, contentType: "image/png" });
    const openedAt = Date.now();
    await expect.poll(() => Date.now(), { timeout: 70000 }).toBeGreaterThan(openedAt + 65000);
    await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
    expect(await frame.getAttribute("src")).toBe(source);
    await expect(pageNumber).toHaveValue("2");
    expect(await viewerFrame!.evaluate(() => performance.timeOrigin)).toBe(timeOrigin);
    const after = await page.screenshot({ path: testInfo.outputPath("pdf-page-two-after.png") });
    await testInfo.attach("pdf-page-two-after-65s", { body: after, contentType: "image/png" });
  } finally { await api.cleanup(); }
});
