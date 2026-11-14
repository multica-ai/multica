import { test, expect } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { createTestApi, loginAsDefault, waitForPageText } from "./helpers";

test.use({ viewport: { width: 1440, height: 900 }, video: { mode: "on", size: { width: 1440, height: 900 } } });

test("pins navigate without duplicating visible originals and persist across clients", async ({ page, context }, testInfo) => {
  test.setTimeout(300_000);
  const api = await createTestApi();
  try {
    const issue = await api.createIssue("Pinned discussion decisions");
    const root = await api.createComment(issue.id, "Decision: keep the timeline chronological. Pins are navigation only.");
    const reply = await api.createComment(issue.id, "Release checklist: verify persistence and realtime updates.", root.id);
    for (let i = 0; i < 30; i++) await api.createComment(issue.id, `Discussion ${i + 1}\n\n${"Synthetic discussion detail for scrolling. ".repeat(12)}`);
    const slug = await loginAsDefault(page);
    await page.goto(`/${slug}/issues/${issue.id}`, { waitUntil: "domcontentloaded" });
    await waitForPageText(page, "Pinned discussion decisions");
    // The timeline loads after the title, so wait for a scrollable timeline
    // before driving scrollTop.
    const scrollPage = async (target: typeof page, where: "top" | "bottom") => expect(async () => target.evaluate(where => {
      const node = document.querySelector("[data-comment-pins-overlay]");
      let parent = node?.parentElement;
      while (parent && parent.scrollHeight <= parent.clientHeight + 20) parent = parent.parentElement;
      if (!parent) throw new Error("timeline scroller missing");
      parent.scrollTop = where === "top" ? Math.max(0, (node as HTMLElement).offsetTop - 100) : parent.scrollHeight;
    }, where).then(async () => {
      // After a reload the page restores its saved reading position once the
      // timeline mounts; re-check so that restore cannot undo this scroll.
      await target.waitForTimeout(500);
      const atBottom = await target.evaluate(() => {
        let p = document.querySelector("[data-comment-pins-overlay]")?.parentElement;
        while (p && p.scrollHeight <= p.clientHeight + 20) p = p.parentElement;
        return !!p && p.scrollTop + p.clientHeight >= p.scrollHeight - 200;
      });
      if (where === "bottom" && !atBottom) throw new Error("scroll was reset");
    })).toPass({ timeout: 30_000 });
    const scroll = (where: "top" | "bottom") => scrollPage(page, where);
    // Existing deep-link navigation mounts the oldest virtualized thread.
    await page.goto(`/${slug}/issues/${issue.id}#comment-${root.id}`);
    const body = page.locator(`[data-comment-pin-body="${root.id}"]`);
    await expect(body).toBeVisible({ timeout: 30_000 });
    // The deep-link landing re-renders the thread while it settles, which can
    // detach an already-open context menu; reopen until the click lands.
    await expect(async () => {
      await page.keyboard.press("Escape");
      await body.click({ button: "right" });
      await page.getByRole("menuitem", { name: "Pin comment", exact: true }).click({ timeout: 5_000 });
    }).toPass({ timeout: 60_000 });
    await expect.poll(async () => {
      const res = await page.request.get(`${process.env.NEXT_PUBLIC_API_URL}/api/issues/${issue.id}/timeline`, { headers: { Authorization: `Bearer ${api.getToken()}`, "X-Workspace-ID": (await api.getWorkspaces())[0]!.id } });
      const entries = await res.json();
      return entries.find((e: { id: string }) => e.id === root.id)?.pinned_at;
    }).toBeTruthy();
    // Leave the deep-link landing mode before testing ordinary scrolling.
    await page.goto(`/${slug}/issues/${issue.id}`, { waitUntil: "domcontentloaded" });
    await waitForPageText(page, "Pinned discussion decisions");
    await scroll("bottom");
    const bar = page.getByTestId("comment-pins");
    await expect(bar).toBeVisible();
    const evidence = process.env.PIN_EVIDENCE_DIR;
    if (evidence) { await mkdir(evidence, { recursive: true }); await page.screenshot({ path: `${evidence}/collapsed.png`, animations: "disabled" }); }
    const rootRow = bar.locator(`[data-pin-row="${root.id}"]`);
    await expect(rootRow).not.toHaveAttribute("data-on-screen");
    await rootRow.click();
    await expect(body).toBeInViewport();
    // The original is on screen: its row stays in place, dimmed, so the bar
    // never changes height while reading.
    await expect(rootRow).toHaveAttribute("data-on-screen", "true");
    await expect(bar).toBeVisible();
    await scroll("bottom");
    await expect(rootRow).not.toHaveAttribute("data-on-screen");
    await page.reload();
    await scroll("bottom");
    await expect(bar).toBeVisible({ timeout: 30_000 });
    const second = await context.newPage();
    await second.goto(`/${slug}/issues/${issue.id}`);
    await scrollPage(second, "bottom");
    await expect(second.getByTestId("comment-pins")).toBeVisible({ timeout: 30_000 });
    const workspaces = await api.getWorkspaces();
    const headers = { Authorization: `Bearer ${api.getToken()}`, "X-Workspace-ID": workspaces[0]!.id };
    const pinReply = await page.request.post(`${process.env.NEXT_PUBLIC_API_URL}/api/comments/${reply.id}/pin`, { headers });
    expect(pinReply.ok()).toBe(true);
    await expect(second.getByTestId("comment-pins")).toContainText("Release checklist", { timeout: 15_000 });
    const unpin = await page.request.delete(`${process.env.NEXT_PUBLIC_API_URL}/api/comments/${root.id}/pin`, { headers });
    expect(unpin.ok()).toBe(true);
    await expect.poll(async () => (await second.getByTestId("comment-pins").allTextContents()).join(" ")).not.toContain("Decision:");
    // Pins stack in pin order: the reply pinned later sits below.
    await expect(second.locator("[data-pin-row]")).toHaveCount(1);
    // Unpin from the UI: jump to the pinned reply through the bar, then use
    // the comment menu. Both clients drop the bar once the last pin is gone.
    await second.locator(`[data-pin-row="${reply.id}"]`).click();
    const replyBody = second.locator(`[data-comment-pin-body="${reply.id}"]`);
    await expect(replyBody).toBeInViewport({ timeout: 15_000 });
    await expect(async () => {
      await second.keyboard.press("Escape");
      await replyBody.click({ button: "right" });
      await second.getByRole("menuitem", { name: "Unpin comment", exact: true }).click({ timeout: 5_000 });
    }).toPass({ timeout: 60_000 });
    await expect(second.getByTestId("comment-pins")).toHaveCount(0, { timeout: 15_000 });
    await expect(page.getByTestId("comment-pins")).toHaveCount(0, { timeout: 15_000 });
    await expect.poll(async () => {
      const res = await page.request.get(`${process.env.NEXT_PUBLIC_API_URL}/api/issues/${issue.id}/timeline`, { headers });
      return (await res.json()).filter((e: { pinned_at?: string | null }) => e.pinned_at).length;
    }).toBe(0);
    await second.close();
    await testInfo.attach("pin navigation video path", { body: await page.video()!.path(), contentType: "text/plain" });
  } finally { await api.cleanup(); }
});
