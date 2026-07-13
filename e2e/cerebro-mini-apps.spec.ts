import { expect, test } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import pg from "pg";
import { createTestApi, loginAsDefault } from "./helpers";

const appID = "f1540000-0000-4154-8154-000000000001";
const databaseURL = process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";
const fixtureRoot = join(process.cwd(), "apps/cerebro-apps-runtime/fixtures", appID, "1.0.0", "frontend");

test("opens the allergen app and completes its demo workflow", async ({ page }) => {
  const api = await createTestApi();
  await api.setWorkspaceFeatureFlag("cerebro_mini_apps", true);
  const db = new pg.Client(databaseURL);
  await db.connect();
  try {
    await seedFixtures(db, api.getWorkspaceId()!, api.getUserId()!);
    await page.route(`**/api/cerebro/apps-runtime/apps/${appID}/1.0.0/`, async (route) => route.fulfill({ contentType: "text/html", body: await readFile(join(fixtureRoot, "index.html"), "utf8") }));
    await page.route(`**/api/cerebro/apps-runtime/apps/${appID}/1.0.0/app.js`, async (route) => route.fulfill({ contentType: "text/javascript", body: await readFile(join(fixtureRoot, "app.js"), "utf8") }));
    await page.route(`**/api/cerebro/apps/${appID}/token`, async (route) => route.fulfill({ contentType: "application/json", body: JSON.stringify({ key: "sk_e2e", session_id: "session-e2e", expires_at: "2099-01-01T00:00:00Z" }) }));
    await page.route(`**/api/cerebro/apps-runtime/workers/${appID}/1.0.0/invoke`, async (route) => route.fulfill({ contentType: "application/json", body: JSON.stringify({ formatted_ingredients: "WHEAT flour, MILK", allergens: ["WHEAT", "MILK"] }) }));

    const slug = await loginAsDefault(page);
    await page.goto(`/${slug}/apps`);
    await page.getByRole("link", { name: /Allergen Formatter/ }).click();
    const frame = page.frameLocator(`iframe[title="Allergen Formatter"]`);
    await frame.getByLabel("Ingredients").fill("wheat flour, milk");
    await frame.getByRole("button", { name: "Format ingredients" }).click();
    await expect(frame.getByText(/WHEAT flour, MILK/)).toBeVisible();

    await page.getByRole("button", { name: "Test workflow" }).click();
    await expect(page.getByText("Workflow test succeeded")).toBeVisible();
  } finally {
    await db.query("DELETE FROM cerebro_app WHERE id=$1", [appID]);
    await api.setWorkspaceFeatureFlag("cerebro_mini_apps", false);
    await db.end();
  }
});

test("keeps the app catalog usable at phone width", async ({ page }) => {
  const api = await createTestApi();
  await api.setWorkspaceFeatureFlag("cerebro_mini_apps", true);
  try {
    await page.setViewportSize({ width: 390, height: 844 });
    const slug = await loginAsDefault(page);
    await page.goto(`/${slug}/apps`);
    await expect(page.getByRole("link", { name: "Build app" })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  } finally {
    await api.setWorkspaceFeatureFlag("cerebro_mini_apps", false);
  }
});

async function seedFixtures(db: pg.Client, workspaceID: string, userID: string) {
  await db.query(`INSERT INTO cerebro_app (id,workspace_id,slug,name,description,icon,folder,owner_id,current_version,status)
    VALUES ($1,$2,'allergen-formatter','Allergen Formatter','Format ingredients and return allergens','blocks','Operations',$3,'1.0.0','published')
    ON CONFLICT (id) DO UPDATE SET workspace_id=EXCLUDED.workspace_id,owner_id=EXCLUDED.owner_id,current_version='1.0.0',status='published'`, [appID, workspaceID, userID]);
  const snapshot = { manifest: { schema_version: "1", name: "Allergen Formatter", version: "1.0.0", scopes: [{ resource_type: "integration", resource_id: "ai_gateway", access: "write" }] }, frontend: { entry: "frontend/index.html" }, backend: { entry: "backend/index.mjs" } };
  await db.query(`INSERT INTO cerebro_app_version (app_id,version,content_snapshot,release_notes,created_by) VALUES ($1,'1.0.0',$2,'Initial fixture',$3) ON CONFLICT (app_id,version) DO UPDATE SET content_snapshot=EXCLUDED.content_snapshot`, [appID, snapshot, userID]);
  await db.query(`INSERT INTO cerebro_app_grant (app_id,version,scopes,status,approved_by,approved_at) VALUES ($1,'1.0.0',$2,'approved',$3,now()) ON CONFLICT (app_id,version) DO UPDATE SET scopes=EXCLUDED.scopes,status='approved',approved_by=EXCLUDED.approved_by,approved_at=now()`, [appID, snapshot.manifest.scopes, userID]);
  await db.query("DELETE FROM cerebro_app_workflow_def WHERE app_id=$1", [appID]);
  await db.query(`INSERT INTO cerebro_app_workflow_def (workspace_id,app_id,name,definition,version,enabled,owner_id) VALUES ($1,$2,'Allergen review',$3,'1.0.0',false,$4)`, [workspaceID, appID, { schema_version: "1", trigger: { id: "trigger", type: "manual", config: {} }, steps: [{ id: "read", type: "registry.read", config: { resource_id: "products" } }, { id: "filter", type: "filter", config: { field: "read.count", operator: "gt", value: 0 } }, { id: "write", type: "registry.write", config: { resource_id: "products" } }] }, userID]);
}
