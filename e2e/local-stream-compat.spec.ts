import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";

const apiBase = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;

test("native local audio survives capability renewal", async ({ page, baseURL }, testInfo) => {
  test.setTimeout(120000);
  const local = (url: string) => ["localhost", "127.0.0.1", "[::1]"].includes(new URL(url).hostname);
  test.skip(!local(baseURL!) || !local(apiBase) || !!process.env.S3_BUCKET, "Isolated LocalStorage fixtures only");
  const api = await createTestApi();
  try {
    const slug = await loginAsDefault(page);
    const issue = await api.createIssue("Local streamed attachment");
    const bytes = Buffer.alloc(44 + 8000 * 2 * 120);
    bytes.write("RIFF", 0); bytes.writeUInt32LE(bytes.length - 8, 4);
    bytes.write("WAVEfmt ", 8); bytes.writeUInt32LE(16, 16);
    bytes.writeUInt16LE(1, 20); bytes.writeUInt16LE(1, 22);
    bytes.writeUInt32LE(8000, 24); bytes.writeUInt32LE(16000, 28);
    bytes.writeUInt16LE(2, 32); bytes.writeUInt16LE(16, 34);
    bytes.write("data", 36); bytes.writeUInt32LE(bytes.length - 44, 40);
    const form = new FormData();
    form.append("file", new Blob([bytes], { type: "audio/wav" }), "renewal.wav");
    form.append("issue_id", issue.id);
    const headers = { Authorization: `Bearer ${api.getToken()}`, "X-Workspace-Slug": slug };
    const uploaded = await fetch(`${apiBase}/api/upload-file`, { method: "POST", headers, body: form });
    expect(uploaded.status).toBe(200);
    const att = await uploaded.json();
    await page.goto(`/${slug}/attachments/${att.id}/preview`);
    const audio = page.locator("audio");
    await expect(audio).toBeVisible();
    await expect.poll(() => audio.evaluate((el: HTMLAudioElement) => el.readyState)).toBeGreaterThan(0);
    const first = (await audio.getAttribute("src"))!;
    expect(first).toContain("/signed-download?");
    const expires = Number(new URL(first).searchParams.get("exp"));
    expect(expires * 1000 - Date.now()).toBeLessThanOrEqual(60000);
    await page.locator("body").click({ position: { x: 10, y: 100 } });
    await audio.evaluate(async (el: HTMLAudioElement) => { el.currentTime = 12; await el.play(); });
    // A loaded stream must not be replaced just because its capability expires.
    await expect.poll(() => Date.now(), { timeout: 70000 }).toBeGreaterThan((expires + 2) * 1000);
    await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
    expect(await audio.getAttribute("src")).toBe(first);
    expect(await audio.evaluate((el: HTMLAudioElement) => el.currentTime)).toBeGreaterThan(60);
    expect((await fetch(first)).status).toBe(403);
    // A late native error renews on demand. The first metadata request fails
    // to cover bounded recovery from a transient network outage as well.
    let outage = true;
    await page.route(`**/api/attachments/${att.id}`, async (route) => {
      if (outage) { outage = false; await route.abort("failed"); }
      else await route.continue();
    });
    const position = await audio.evaluate((el: HTMLAudioElement) => el.currentTime);
    await audio.dispatchEvent("error");
    await expect.poll(() => audio.getAttribute("src"), { timeout: 10000 }).not.toBe(first);
    await expect.poll(() => audio.evaluate((el: HTMLAudioElement) => el.readyState)).toBeGreaterThan(0);
    expect(await audio.evaluate((el: HTMLAudioElement) => el.currentTime)).toBeGreaterThanOrEqual(position - 1);
    expect(await audio.evaluate((el: HTMLAudioElement) => el.paused)).toBe(false);
    await testInfo.attach("stream-after-renewal", { body: await page.screenshot(), contentType: "image/png" });
  } finally { await api.cleanup(); }
});
