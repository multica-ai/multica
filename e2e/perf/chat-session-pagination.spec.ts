import { expect, test } from "@playwright/test";
import { setupSessionPagination } from "./fixtures/chat-session-pagination";

const baseline = process.env.CHAT_PAGINATION_BASELINE === "1";
test.use({ viewport: { width: 1280, height: 800 }, locale: "en-US", serviceWorkers: "block" });

for (const surface of ["history", "archive", "floating"] as const) {
  test(`session pagination: ${surface}`, async ({ page, baseURL }) => {
    const fixture = await setupSessionPagination(page, baseURL!);
    if (surface === "floating") await page.addInitScript(() => localStorage.setItem("multica:chat:isOpen", "true"));
    const started = Date.now();
    await page.goto(surface === "floating" ? "/perf/inbox" : "/perf/chat");
    if (surface === "floating") await page.getByRole("button", { name: "New chat", exact: true }).click();
    await expect(page.getByText("Session 0000", { exact: true })).toBeVisible();
    const firstReady = Date.now() - started;
    const initialRequests = [...fixture.requests];
    if (!baseline) {
      expect(initialRequests.some((r) => r.path === "/api/chat/sessions?status=all")).toBe(false);
      expect(initialRequests.filter((r) => r.path.includes("status=archived"))).toHaveLength(0);
      expect(initialRequests.find((r) => r.path.includes("/page?"))?.rows).toBe(50);
    }
    await page.screenshot({ path: test.info().outputPath("initial.png") });
    if (surface === "archive") {
      await page.getByRole("button", { name: /Archived.*1500/ }).click();
      await expect(page.getByText("Archived 0000", { exact: true })).toBeVisible();
      if (!baseline) expect(fixture.requests.filter((r) => r.path.includes("status=archived"))).toHaveLength(1);
    }
    if (!baseline) {
      await page.getByRole("button", { name: "Load more", exact: true }).click();
      await expect.poll(() => fixture.requests.filter((r) => r.path.includes("cursor=50")).length).toBe(1);
    }
    const report = { baseline, surface, first_ready_ms: firstReady, initial_bytes: initialRequests.reduce((sum, r) => sum + r.bytes, 0), initial_requests: initialRequests, requests: fixture.requests };
    console.log(JSON.stringify(report));
    await test.info().attach("pagination", { body: JSON.stringify(report, null, 2), contentType: "application/json" });
    expect(fixture.errors).toEqual([]);
    expect(fixture.unexpected).toEqual([]);
  });
}

test("an old session deep link survives the first page", async ({ page, baseURL }) => {
  const fixture = await setupSessionPagination(page, baseURL!);
  await page.goto("/perf/chat?session=aaaa0499-4444-4444-8444-444444444444");
  await expect(page.getByText("Session 0000", { exact: true })).toBeVisible();
  await expect(page).toHaveURL(/session=aaaa0499/);
  await expect(page.getByText("Session 0499", { exact: true })).toBeVisible();
  if (!baseline) await expect.poll(() => fixture.requests.some((r) => r.path === "/api/chat/sessions/aaaa0499-4444-4444-8444-444444444444")).toBe(true);
  expect(fixture.errors).toEqual([]);
  expect(fixture.unexpected).toEqual([]);
});

test("an empty active page still offers the archive", async ({ page, baseURL }) => {
  test.skip(baseline, "pagination regression");
  const fixture = await setupSessionPagination(page, baseURL!, { active: 0, archived: 1 });
  await page.goto("/perf/chat");
  await page.getByRole("button", { name: /Archived.*1/ }).click();
  await expect(page.getByText("Archived 0000", { exact: true })).toBeVisible();
  expect(fixture.errors).toEqual([]);
  expect(fixture.unexpected).toEqual([]);
});
