import { expect, test } from "@playwright/test";
import pg from "pg";

import { createTestApi, loginAsDefault } from "./helpers";

test("opens a permission detail from Settings > Permissions", async ({ page }) => {
  const api = await createTestApi();
  const db = new pg.Client(process.env.DATABASE_URL!);
  await db.connect();

  try {
    await api.setWorkspaceFeatureFlag("cerebro_permission_detail", true);
    await db.query(
      `INSERT INTO cerebro_capability
       (workspace_id, capability_key, title, category, description, source)
       VALUES ($1, 'tools:FIR3199', 'FIR 3199 Test Tool', 'Built-in tools',
               'Permission detail navigation test fixture', 'e2e')
       ON CONFLICT (workspace_id, capability_key)
       DO UPDATE SET title = EXCLUDED.title, last_reported_at = NOW()`,
      [api.getWorkspaceId()],
    );
    const workspaceSlug = await loginAsDefault(page);

    await page.goto(`/${workspaceSlug}/settings?tab=permissions`);

    const permissionLink = page.getByRole("button", {
      name: "Open details for FIR 3199 Test Tool",
    });
    await expect(permissionLink).toBeVisible();
    await permissionLink.click();

    await expect(page).toHaveURL(/\/cerebro\/permissions\/.+/);
    await expect(page.getByRole("tab", { name: "Who & why" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "Changes" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "Usage" })).toBeVisible();
  } finally {
    await db.query(
      `DELETE FROM cerebro_capability
       WHERE workspace_id = $1 AND capability_key = 'tools:FIR3199'`,
      [api.getWorkspaceId()],
    );
    await db.end();
    await api.setWorkspaceFeatureFlag("cerebro_permission_detail", false);
    await api.cleanup();
  }
});
