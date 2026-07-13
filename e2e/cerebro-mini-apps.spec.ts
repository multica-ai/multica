import { expect, test } from "@playwright/test";
import { createServer, type Server } from "node:http";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import pg from "pg";
import { createTestApi, loginAsDefault } from "./helpers";

const appID = "f1540000-0000-4154-8154-000000000001";
const databaseURL = process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";
// The web proxy at /api/cerebro/apps-runtime forwards to this port, so the real
// runtime has to answer there for the app to load and its worker to run.
const runtimePort = Number(new URL(process.env.CEREBRO_APPS_RUNTIME_URL ?? "http://127.0.0.1:4310").port);

test("opens the allergen app and completes its demo workflow", async ({ page }) => {
  const api = await createTestApi();
  await api.setWorkspaceFeatureFlag("cerebro_mini_apps", true);
  const db = new pg.Client(databaseURL);
  await db.connect();
  const gateway = await startStubGateway();
  const runtime = await startAppsRuntime();
  try {
    await seedFixtures(db, api.getWorkspaceId()!, api.getUserId()!);
    // Only the token broker is stubbed — it fronts the Firtal registry, which an
    // e2e run must not call. Everything the app itself does stays real: page and
    // script come from the runtime, and the worker really runs and really calls
    // the gateway with the payload the frontend built. That keeps the
    // frontend/worker contract under test instead of asserting on a mock.
    await page.route(`**/api/cerebro/apps/${appID}/token`, async (route) => route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ key: "sk_e2e", session_id: "session-e2e", expires_at: "2099-01-01T00:00:00Z", ai_base_url: gateway.url }),
    }));

    const slug = await loginAsDefault(page);
    await page.goto(`/${slug}/apps`);
    await page.getByRole("link", { name: /Allergen Formatter/ }).click();
    const frame = page.frameLocator(`iframe[title="Allergen Formatter"]`);
    await frame.getByLabel("Ingredients").fill("wheat flour, milk");
    await frame.getByRole("button", { name: "Format ingredients" }).click();
    await expect(frame.getByText(/WHEAT flour, MILK/)).toBeVisible();
    // The worker reached the gateway on the app's personal key, which only
    // happens when the frontend sent every field the backend requires.
    expect(gateway.calls()).toEqual(["Bearer sk_e2e"]);

    await page.getByRole("button", { name: "Test workflow" }).click();
    await expect(page.getByText("Workflow test succeeded")).toBeVisible();
  } finally {
    await db.query("DELETE FROM cerebro_app WHERE id=$1", [appID]);
    await api.setWorkspaceFeatureFlag("cerebro_mini_apps", false);
    await db.end();
    await runtime.close();
    await gateway.close();
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

async function startAppsRuntime() {
  const { createAppsRuntime } = await import(pathToFileURL(join(process.cwd(), "apps/cerebro-apps-runtime/runtime.mjs")).href);
  const runtime = createAppsRuntime({
    bundleRoot: join(process.cwd(), "apps/cerebro-apps-runtime/fixtures"),
    frameAncestors: process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:3000",
  });
  const server = createServer(async (req, res) => {
    const chunks: Buffer[] = [];
    for await (const chunk of req) chunks.push(chunk as Buffer);
    const response: Response = await runtime.fetch(new Request(`http://127.0.0.1${req.url ?? "/"}`, {
      method: req.method,
      headers: req.headers as Record<string, string>,
      body: chunks.length ? Buffer.concat(chunks) : undefined,
    }));
    res.writeHead(response.status, Object.fromEntries(response.headers.entries()));
    res.end(Buffer.from(await response.arrayBuffer()));
  });
  await listen(server, runtimePort);
  return { close: () => close(server) };
}

async function startStubGateway() {
  const calls: string[] = [];
  const server = createServer((req, res) => {
    calls.push(req.headers.authorization ?? "");
    res.setHeader("content-type", "application/json");
    res.end(JSON.stringify({ choices: [{ message: { content: JSON.stringify({ formatted_ingredients: "WHEAT flour, MILK", allergens: ["WHEAT", "MILK"] }) } }] }));
  });
  await listen(server, 0);
  const address = server.address();
  if (typeof address === "string" || address === null) throw new Error("stub gateway did not bind a port");
  return { url: `http://127.0.0.1:${address.port}`, calls: () => calls, close: () => close(server) };
}

function listen(server: Server, port: number) {
  return new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", resolve);
  });
}

function close(server: Server) {
  return new Promise<void>((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
}

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
