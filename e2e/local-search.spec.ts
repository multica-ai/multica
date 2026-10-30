import { expect, test, type Page, type Response } from "@playwright/test";
import { TestApiClient } from "./fixtures";
import { createTestApi, loginAsDefault, openWorkspaceMenu } from "./helpers";

// Web search answers from the local search index once it has synced
// (MUL-7754): results match server search, follow writes made elsewhere, and
// no server search request is sent while typing.

const SEARCH_PLACEHOLDER = "Type a command or search...";

function isSearchIndexChanges(res: Response): boolean {
  return res.url().includes("/api/search-index/changes") && res.ok();
}

async function openSearch(page: Page) {
  await page.keyboard.press("ControlOrMeta+k");
  await expect(page.getByPlaceholder(SEARCH_PLACEHOLDER)).toBeVisible();
}

async function search(page: Page, query: string) {
  const input = page.getByPlaceholder(SEARCH_PLACEHOLDER);
  await input.fill("");
  await input.fill(query);
}

function searchIndexDatabases(page: Page): Promise<string[]> {
  return page.evaluate(async () =>
    (await indexedDB.databases())
      .map((db) => db.name ?? "")
      .filter((name) => name.startsWith("multica-search-index:")),
  );
}

/** Identifiers of the listed issues, in the order the palette shows them. */
async function visibleIdentifiers(page: Page, candidates: string[]): Promise<string[]> {
  const texts = await page.getByRole("option").allInnerTexts();
  return texts
    .map((text) => candidates.find((identifier) => new RegExp(`\\b${identifier}\\b`).test(text)))
    .filter((identifier): identifier is string => identifier !== undefined);
}

test.describe("Local search index", () => {
  let api: TestApiClient;

  test.beforeEach(async () => {
    api = await createTestApi();
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("answers from the local index with server ranking and follows remote writes", async ({ page }) => {
    const token = `lsi${Date.now().toString(36)}`;
    const exact = await api.createIssue(`${token} release plan`);
    await api.createIssue(`plan the ${token} release`);
    const spread = await api.createIssue(`${token} notes`, { description: "release plan draft" });
    const commented = await api.createIssue("unrelated title");
    await api.createComment(commented.id, `${token} release plan in one comment`);
    await api.createComment(spread.id, "another remark");

    const serverSearches: string[] = [];
    page.on("request", (req) => {
      if (/\/api\/(issues|projects)\/search/.test(req.url())) serverSearches.push(req.url());
    });

    const synced = page.waitForResponse(isSearchIndexChanges, { timeout: 45_000 });
    await loginAsDefault(page);
    await synced;
    // The worker reports readiness right after its first catch-up completes.
    await page.waitForTimeout(500);

    await openSearch(page);
    const query = `${token} release plan`;
    await search(page, query);
    const server = await api.searchIssues(query);
    const expected = server.issues.map((issue) => issue.identifier);
    expect(server.issues[0]?.title).toBe(exact.title);
    expect(expected).toHaveLength(4);
    await expect.poll(() => visibleIdentifiers(page, expected)).toEqual(expected);
    expect(serverSearches).toEqual([]);

    // A write made elsewhere reaches the local copy through the change log.
    const caughtUp = page.waitForResponse(isSearchIndexChanges, { timeout: 30_000 });
    const fresh = await api.createIssue(`${token} fresh arrival`);
    await caughtUp;
    await search(page, `${token} fresh`);
    await expect(page.getByRole("option").filter({ hasText: `${token} fresh arrival` })).toBeVisible();

    const removed = page.waitForResponse(isSearchIndexChanges, { timeout: 30_000 });
    await api.deleteIssue(fresh.id);
    await removed;
    await search(page, `${token} fresh`);
    await expect(page.getByRole("option").filter({ hasText: `${token} fresh arrival` })).toHaveCount(0);

    expect(serverSearches).toEqual([]);
  });

  test("deletes the local copy on logout", async ({ page }) => {
    const synced = page.waitForResponse(isSearchIndexChanges, { timeout: 45_000 });
    await loginAsDefault(page);
    await synced;
    await expect.poll(() => searchIndexDatabases(page)).toHaveLength(1);

    await openWorkspaceMenu(page);
    await page.getByRole("menuitem", { name: "Log out" }).click();
    await page.waitForURL("**/login", { timeout: 10_000, waitUntil: "domcontentloaded" });

    await expect.poll(() => searchIndexDatabases(page)).toEqual([]);
  });

  test("deletes a workspace's local copy when the user is removed from it", async ({ page }) => {
    const run = Date.now().toString(36);
    const owner = new TestApiClient();
    await owner.login(`e2e-owner-${run}@multica.ai`, "E2E Owner");
    const shared = await owner.ensureWorkspace(`E2E Shared ${run}`, `e2e-shared-${run}`);
    await owner.markUserOnboarded();
    await owner.createIssue(`shared ${run} issue`);
    try {
      const memberId = await owner.addMemberByEmail(api.getEmail());
      await loginAsDefault(page);

      const synced = page.waitForResponse(isSearchIndexChanges, { timeout: 45_000 });
      await page.goto(`/${shared.slug}/issues`, { waitUntil: "domcontentloaded" });
      await synced;
      const sharedCopy = (names: string[]) => names.some((name) => name.endsWith(`:${shared.id}`));
      await expect.poll(async () => sharedCopy(await searchIndexDatabases(page))).toBe(true);

      await owner.removeMember(memberId);

      // The client relocates to a workspace the user still has; the removed
      // workspace's copy must not survive on the device. A read that fails
      // mid-navigation counts as "still there", so polling continues until a
      // successful read confirms the deletion.
      const unreadable = [`multica-search-index:unreadable:${shared.id}`];
      await expect
        .poll(async () => sharedCopy(await searchIndexDatabases(page).catch(() => unreadable)), { timeout: 20_000 })
        .toBe(false);
    } finally {
      await owner.cleanup();
      await owner.deleteWorkspace();
    }
  });
});
