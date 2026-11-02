import "./env";

import { expect, test } from "@playwright/test";
import pg from "pg";

import { createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";
const SHOT_DIR = process.env.SKILL_FOLDERS_SCREENSHOT_DIR;

test("skills page folder explorer: nested in-place expansion and persistence", async ({ page }) => {
  test.setTimeout(120000);
  const api: TestApiClient = await createTestApi();
  const db = new pg.Client(DATABASE_URL);
  await db.connect();
  const suffix = String(Date.now());
  const rootName = `Engineering ${suffix}`;
  const childName = `Frontend ${suffix}`;
  const emptyName = `Empty ${suffix}`;
  const alphaName = `Alpha skill ${suffix}`;
  const nestedName = `Nested skill ${suffix}`;
  const uncategorizedName = `Root skill ${suffix}`;
  const skillIds: string[] = [];

  try {
    const workspace = (await api.getWorkspaces())[0];
    if (!workspace) throw new Error("E2E workspace missing");
    api.setWorkspaceId(workspace.id);
    api.setWorkspaceSlug(workspace.slug);

    const root = await api.postJson<{ id: string }>("/api/skill-folders", {
      name: rootName,
      parent_id: "",
    });
    for (const name of [alphaName, nestedName, uncategorizedName]) {
      const skill = await api.postJson<{ id: string }>("/api/skills", {
        name,
        description: "",
        content: `# ${name}\n`,
        config: {},
      });
      skillIds.push(skill.id);
    }
    const [alphaId, nestedId] = skillIds as [string, string, string];
    await db.query(
      `INSERT INTO labrastro_skill_placement (workspace_id, skill_id, folder_id)
       VALUES ($1, $2, $3)`,
      [workspace.id, alphaId, root.id],
    );

    const token = api.getToken();
    if (!token) throw new Error("E2E token missing");
    await page.addInitScript((authToken) => {
      localStorage.setItem("multica_token", authToken);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, token);
    await page.setViewportSize({ width: 1600, height: 1000 });
    await page.goto(`/${workspace.slug}/skills`, { waitUntil: "domcontentloaded" });

    await expect(page.getByRole("button", { name: `Expand ${rootName}` })).toBeVisible({ timeout: 30000 });
    await expect(page.locator('[role="row"]', { hasText: uncategorizedName })).toBeVisible();
    await expect(page.locator('[role="row"]', { hasText: alphaName })).toHaveCount(0);

    await page.getByRole("button", { name: `Expand ${rootName}` }).click();
    await expect(page.locator('[role="row"]', { hasText: alphaName })).toBeVisible();
    await expect(page.locator('[role="row"]', { hasText: uncategorizedName })).toBeVisible();
    await page.getByRole("button", { name: `Collapse ${rootName}` }).click();
    await expect(page.locator('[role="row"]', { hasText: alphaName })).toHaveCount(0);

    await page.getByRole("button", { name: `Actions for ${rootName}` }).click();
    await page.getByRole("menuitem", { name: "New subfolder" }).click();
    await page.getByLabel("Name").fill(childName);
    await page.getByRole("button", { name: "Create" }).click();
    await expect(page.getByRole("button", { name: `Collapse ${rootName}` })).toBeVisible();
    await expect(page.getByRole("button", { name: `Expand ${childName}` })).toBeVisible();
    const child = await db.query<{ id: string }>(
      `SELECT id::text FROM labrastro_skill_folder WHERE workspace_id = $1 AND name = $2`,
      [workspace.id, childName],
    );
    const childId = child.rows[0]?.id;
    if (!childId) throw new Error("Created child folder missing");
    await db.query(
      `INSERT INTO labrastro_skill_placement (workspace_id, skill_id, folder_id)
       VALUES ($1, $2, $3)`,
      [workspace.id, nestedId, childId],
    );

    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByRole("button", { name: `Collapse ${rootName}` })).toBeVisible({ timeout: 30000 });
    await page.getByRole("button", { name: `Expand ${childName}` }).click();
    await expect(page.locator('[role="row"]', { hasText: nestedName })).toBeVisible();
    await expect(page.locator('[role="row"]', { hasText: alphaName })).toBeVisible();
    await expect(page.locator('[role="row"]', { hasText: uncategorizedName })).toBeVisible();
    expect(page.url()).not.toContain("folder=");
    if (SHOT_DIR) await page.screenshot({ path: `${SHOT_DIR}/01-nested-tree.png`, fullPage: true });

    await page.getByRole("button", { name: "Collapse all" }).click();
    await expect(page.locator('[role="row"]', { hasText: alphaName })).toHaveCount(0);
    await expect(page.locator('[role="row"]', { hasText: nestedName })).toHaveCount(0);
    await page.getByRole("button", { name: "Expand all" }).click();
    await expect(page.locator('[role="row"]', { hasText: nestedName })).toBeVisible();

    const search = page.getByRole("textbox", { name: "Search skills…" });
    await search.fill(nestedName);
    await expect(page.getByText(`${rootName} / ${childName}`, { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: `Collapse ${rootName}` })).toHaveCount(0);
    if (SHOT_DIR) await page.screenshot({ path: `${SHOT_DIR}/02-flat-search.png`, fullPage: true });
    await search.fill("");
    await expect(page.getByRole("button", { name: `Collapse ${rootName}` })).toBeVisible();
    await expect(page.getByRole("button", { name: `Collapse ${childName}` })).toBeVisible();

    await page.getByRole("button", { name: "New folder" }).click();
    await page.getByLabel("Name").fill(emptyName);
    await page.getByRole("button", { name: "Create" }).click();
    await expect(page.getByRole("button", { name: `Expand ${emptyName}` })).toBeVisible();

    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByRole("button", { name: `Collapse ${rootName}` })).toBeVisible({ timeout: 30000 });
    await expect(page.getByRole("button", { name: `Collapse ${childName}` })).toBeVisible();
    await expect(page.locator('[role="row"]', { hasText: nestedName })).toBeVisible();
    if (SHOT_DIR) await page.screenshot({ path: `${SHOT_DIR}/03-reload-persisted.png`, fullPage: true });
  } finally {
    await db.query(`DELETE FROM labrastro_skill_placement WHERE skill_id = ANY($1::uuid[])`, [skillIds]);
    await db.query(
      `DELETE FROM labrastro_skill_folder WHERE workspace_id = $1 AND name = ANY($2::text[])`,
      [(await api.getWorkspaces())[0]?.id, [rootName, childName, emptyName]],
    );
    await db.query(`DELETE FROM skill_file WHERE skill_id = ANY($1::uuid[])`, [skillIds]);
    await db.query(`DELETE FROM skill WHERE id = ANY($1::uuid[])`, [skillIds]);
    await db.end();
    await api.cleanup();
  }
});
