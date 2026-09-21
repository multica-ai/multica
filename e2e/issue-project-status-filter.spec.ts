import { expect, test, type Page } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

// "Project status" is a filter dimension of its own next to
// "Project": pick In Progress once instead of ticking every active project.
// The list surface is server-driven, so this is the only place the whole
// chain — menu → store → table query → SQL predicate — is exercised together.

async function openProjectStatusMenu(page: Page) {
  await page.getByRole("button", { name: "Filter", exact: true }).click();
  await page.getByRole("menuitem", { name: "Project status" }).click();
}

async function visibleIssueTitles(page: Page, titles: string[]) {
  const present: string[] = [];
  for (const title of titles) {
    if (await page.getByText(title, { exact: true }).first().isVisible()) {
      present.push(title);
    }
  }
  return present.sort();
}

test.describe("Issue filter: project status", () => {
  let api: TestApiClient;
  const suffix = Date.now().toString(36);
  const activeIssue = `pstatus active ${suffix}`;
  const plannedIssue = `pstatus planned ${suffix}`;
  const orphanIssue = `pstatus no project ${suffix}`;
  const all = [activeIssue, plannedIssue, orphanIssue];

  let plannedProjectId: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    const activeProject = await api.createProject(`pstatus active ${suffix}`, {
      status: "in_progress",
    });
    const plannedProject = await api.createProject(`pstatus planned ${suffix}`, {
      status: "planned",
    });
    plannedProjectId = plannedProject.id;
    await api.createIssue(activeIssue, { project_id: activeProject.id });
    await api.createIssue(plannedIssue, { project_id: plannedProject.id });
    await api.createIssue(orphanIssue);
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("narrows the list to issues whose project has the selected status", async ({
    page,
  }) => {
    await expect
      .poll(() => visibleIssueTitles(page, all))
      .toEqual([...all].sort());

    await openProjectStatusMenu(page);
    const inProgress = page.getByRole("menuitemcheckbox", { name: "In Progress" });
    await inProgress.click();
    // Escape closes the sub-menu, then the root menu. Both have to go before
    // the chips bar underneath is clickable again.
    await page.keyboard.press("Escape");
    await page.keyboard.press("Escape");
    await expect(inProgress).toBeHidden();

    // Only the issue in the in_progress project survives. The projectless
    // issue is out too: with no project there is no status to match.
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([activeIssue]);

    // The chip reports the dimension, and removing it restores the list.
    const chipsBar = page.getByRole("main");
    await expect(chipsBar.getByText("Project status")).toBeVisible();
    await chipsBar.getByRole("button", { name: /Remove .*filter/ }).first().click();
    await expect
      .poll(() => visibleIssueTitles(page, all))
      .toEqual([...all].sort());
  });

  // The issue payloads do not change when a PROJECT's status does, so only a
  // cache invalidation can refresh a window filtered on it — the global
  // staleTime is Infinity. Without one the list stays stale until reload.
  test("picks up a project that moves into the selected status", async ({
    page,
  }) => {
    await openProjectStatusMenu(page);
    const inProgress = page.getByRole("menuitemcheckbox", { name: "In Progress" });
    await inProgress.click();
    await page.keyboard.press("Escape");
    await page.keyboard.press("Escape");
    await expect(inProgress).toBeHidden();
    await expect.poll(() => visibleIssueTitles(page, all)).toEqual([activeIssue]);

    await api.updateProject(plannedProjectId, { status: "in_progress" });

    await expect
      .poll(() => visibleIssueTitles(page, all), { timeout: 15000 })
      .toEqual([activeIssue, plannedIssue].sort());
  });
});
