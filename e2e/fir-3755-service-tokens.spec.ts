import "./env";
import { expect, test } from "@playwright/test";
import pg from "pg";
import { createTestApi, loginAsDefault } from "./helpers";

const API_BASE =
  process.env.NEXT_PUBLIC_API_URL ||
  `http://localhost:${process.env.PORT || "8080"}`;
const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";
const FEATURE_FLAG = "cerebro_service_tokens";
const WORKSPACE_FLAG_USER = "00000000-0000-0000-0000-000000000000";
const FLAG_HYDRATION_TIMEOUT_MS = 30_000;

test("Settings Tokens proves expiry, read-only scope, flag disable, audit, and revoke", async ({
  page,
}) => {
  test.setTimeout(180_000);
  const api = await createTestApi();
  const workspaceId = api.getWorkspaceId();
  if (!workspaceId) throw new Error("E2E workspace was not resolved");

  const database = new pg.Client(DATABASE_URL);
  await database.connect();
  const previousFeatureFlag = await database.query<{ enabled: boolean }>(
    `SELECT enabled
       FROM cerebro_feature_flags
      WHERE workspace_id = $1
        AND user_id = $2
        AND flag_key = $3`,
    [workspaceId, WORKSPACE_FLAG_USER, FEATURE_FLAG],
  );
  const suffix = `${Date.now()}-${test.info().workerIndex}`;
  const tokenName = `FIR-3755 read-only ${suffix}`;
  const forbiddenIssueTitle = `FIR-3755 forbidden write ${suffix}`;
  const readableIssue = await api.createIssue(`FIR-3755 readable ${suffix}`);
  let rawToken = "";

  try {
    await api.setWorkspaceFeatureFlag(FEATURE_FLAG, true);
    const slug = await loginAsDefault(page);
    await page.goto(`/${slug}/settings?tab=tokens`, {
      waitUntil: "domcontentloaded",
    });
    await expect(page).toHaveURL(new RegExp(`/${slug}/settings\\?tab=tokens$`));

    const serviceTokensSection = page.locator("section").filter({
      has: page.getByRole("heading", {
        name: "Service tokens",
        exact: true,
      }),
    });
    // The settings shell performs a fresh authenticated bootstrap after the
    // direct route load. CI can take more than Playwright's default five
    // seconds to hydrate the workspace feature flags, so wait for the actual
    // gated surface before asserting that it is unique.
    await expect(serviceTokensSection).toBeVisible({
      timeout: FLAG_HYDRATION_TIMEOUT_MS,
    });
    await expect(serviceTokensSection).toHaveCount(1);

    for (const scope of ["skills:read", "agents:read", "issues:read"]) {
      await expect(
        serviceTokensSection.getByRole("button", { name: scope, exact: true }),
      ).toHaveCount(1);
    }
    await expect(
      serviceTokensSection.getByText(/:write/, { exact: false }),
    ).toHaveCount(0);
    await expect(
      serviceTokensSection.getByText("No expiry", { exact: true }),
    ).toHaveCount(0);

    const expiry = serviceTokensSection.getByRole("combobox");
    await expect(expiry).toContainText("90");
    await expiry.click();
    for (const option of ["30 days", "90 days", "1 year"]) {
      await expect(
        page.getByRole("option", { name: option, exact: true }),
      ).toHaveCount(1);
    }
    await page.getByRole("option", { name: "30 days", exact: true }).click();
    await expect(expiry).toContainText("30");

    await serviceTokensSection
      .getByPlaceholder("Token name (e.g. Atlas read-only)")
      .fill(tokenName);
    await serviceTokensSection
      .getByRole("button", { name: "skills:read", exact: true })
      .click();
    await serviceTokensSection
      .getByRole("button", { name: "issues:read", exact: true })
      .click();
    await serviceTokensSection
      .getByRole("button", { name: "Create", exact: true })
      .click();

    const createdDialog = page.getByRole("dialog").filter({
      has: page.getByRole("heading", {
        name: "Service token created",
        exact: true,
      }),
    });
    await expect(createdDialog).toBeVisible();
    rawToken = (await createdDialog.locator("code").textContent())?.trim() ?? "";
    expect(rawToken).toMatch(/^msv_[a-f0-9]{40}$/);
    await createdDialog.getByRole("button", { name: "Done", exact: true }).click();
    await expect(createdDialog).toBeHidden();
    await expect(page.getByText(rawToken, { exact: true })).toHaveCount(0);
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(serviceTokensSection).toBeVisible();
    await expect(page.getByText(rawToken, { exact: true })).toHaveCount(0);

    const tokenRow = serviceTokensSection.locator('[data-slot="card"]').filter({
      hasText: tokenName,
    });
    await expect(tokenRow).toHaveCount(1);
    await expect(tokenRow).toContainText("scopes: issues:read");
    await expect(tokenRow).toContainText("expires");

    const tokenRecord = await database.query<{
      id: string;
      remaining_seconds: string;
    }>(
      `SELECT id::text,
              EXTRACT(EPOCH FROM (expires_at - NOW()))::text AS remaining_seconds
         FROM cerebro_service_token
        WHERE workspace_id = $1
          AND name = $2`,
      [workspaceId, tokenName],
    );
    expect(tokenRecord.rowCount).toBe(1);
    const tokenId = tokenRecord.rows[0]!.id;
    const remainingSeconds = Number(tokenRecord.rows[0]!.remaining_seconds);
    expect(remainingSeconds).toBeGreaterThan(29 * 24 * 60 * 60);
    expect(remainingSeconds).toBeLessThanOrEqual(30 * 24 * 60 * 60);

    const issuedAudit = await database.query<{ count: string }>(
      `SELECT COUNT(*)::text AS count
         FROM cerebro_service_token_audit
        WHERE service_token_id = $1
          AND event = 'issued'`,
      [tokenId],
    );
    expect(Number(issuedAudit.rows[0]?.count ?? "0")).toBe(1);

    const readResponse = await fetch(`${API_BASE}/api/service/issues`, {
      headers: { Authorization: `Bearer ${rawToken}` },
    });
    expect(readResponse.status).toBe(200);
    const visibleIssues = (await readResponse.json()) as Array<{ id: string }>;
    expect(visibleIssues.some((issue) => issue.id === readableIssue.id)).toBe(true);

    const deniedWrite = await fetch(`${API_BASE}/api/service/issues`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${rawToken}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ title: forbiddenIssueTitle }),
    });
    expect(deniedWrite.status).toBe(405);
    const forbiddenRows = await database.query<{ count: string }>(
      `SELECT COUNT(*)::text AS count
         FROM issue
        WHERE workspace_id = $1
          AND title = $2`,
      [workspaceId, forbiddenIssueTitle],
    );
    expect(Number(forbiddenRows.rows[0]?.count ?? "0")).toBe(0);

    const usedAudits = await database.query<{
      method: string;
      path: string;
    }>(
      `SELECT detail->>'method' AS method, detail->>'path' AS path
         FROM cerebro_service_token_audit
        WHERE service_token_id = $1
          AND event = 'used'
        ORDER BY created_at`,
      [tokenId],
    );
    expect(usedAudits.rows).toEqual([
      { method: "GET", path: "/api/service/issues" },
      { method: "POST", path: "/api/service/issues" },
    ]);

    await api.setWorkspaceFeatureFlag(FEATURE_FLAG, false);
    const disabledRead = await fetch(`${API_BASE}/api/service/issues`, {
      headers: { Authorization: `Bearer ${rawToken}` },
    });
    expect(disabledRead.status).toBe(401);
    const disabledManagement = await fetch(`${API_BASE}/api/service-tokens`, {
      headers: {
        Authorization: `Bearer ${api.getToken()}`,
        "X-Workspace-ID": workspaceId,
      },
    });
    expect(disabledManagement.status).toBe(404);
    const disabledAuditCount = await database.query<{ count: string }>(
      `SELECT COUNT(*)::text AS count
         FROM cerebro_service_token_audit
        WHERE service_token_id = $1
          AND event = 'used'`,
      [tokenId],
    );
    expect(Number(disabledAuditCount.rows[0]?.count ?? "0")).toBe(2);

    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(
      page.getByRole("heading", { name: "Service tokens", exact: true }),
    ).toHaveCount(0);

    await api.setWorkspaceFeatureFlag(FEATURE_FLAG, true);
    const enabledFlagsResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        response.url().includes(`/workspaces/${workspaceId}/feature-flags`) &&
        response.status() === 200,
      { timeout: FLAG_HYDRATION_TIMEOUT_MS },
    );
    const enabledTokensResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        new URL(response.url()).pathname === "/api/service-tokens" &&
        response.status() === 200,
      { timeout: FLAG_HYDRATION_TIMEOUT_MS },
    );
    await page.reload({ waitUntil: "domcontentloaded" });
    const flagsPayload = (await (await enabledFlagsResponse).json()) as {
      workspace_overrides?: Record<string, boolean>;
    };
    expect(flagsPayload.workspace_overrides?.[FEATURE_FLAG]).toBe(true);
    await enabledTokensResponse;
    await expect(serviceTokensSection).toBeVisible({
      timeout: FLAG_HYDRATION_TIMEOUT_MS,
    });
    await expect(tokenRow).toBeVisible();

    await tokenRow
      .getByRole("button", { name: `Revoke ${tokenName}`, exact: true })
      .click();
    const revokeDialog = page.getByRole("alertdialog").filter({
      has: page.getByRole("heading", {
        name: "Revoke service token?",
        exact: true,
      }),
    });
    await expect(revokeDialog).toBeVisible();
    await revokeDialog
      .getByRole("button", { name: "Revoke", exact: true })
      .click();
    await expect(tokenRow).toContainText("(revoked)");
    await expect(
      tokenRow.getByRole("button", {
        name: `Revoke ${tokenName}`,
        exact: true,
      }),
    ).toHaveCount(0);
    const revokedAudit = await database.query<{ count: string }>(
      `SELECT COUNT(*)::text AS count
         FROM cerebro_service_token_audit
        WHERE service_token_id = $1
          AND event = 'revoked'`,
      [tokenId],
    );
    expect(Number(revokedAudit.rows[0]?.count ?? "0")).toBe(1);

    const revokedRead = await fetch(`${API_BASE}/api/service/issues`, {
      headers: { Authorization: `Bearer ${rawToken}` },
    });
    expect(revokedRead.status).toBe(401);

    for (const evidence of [
      { name: "service-token-revoked-desktop", width: 1280, height: 900 },
      { name: "service-token-revoked-mobile", width: 390, height: 844 },
    ]) {
      await page.setViewportSize({
        width: evidence.width,
        height: evidence.height,
      });
      const screenshot = await page.screenshot({
        fullPage: true,
        path: test.info().outputPath(`${evidence.name}.png`),
      });
      await test.info().attach(evidence.name, {
        body: screenshot,
        contentType: "image/png",
      });
    }
  } finally {
    await database.query(
      `DELETE FROM cerebro_service_token
        WHERE workspace_id = $1
          AND name = $2`,
      [workspaceId, tokenName],
    );
    if (previousFeatureFlag.rowCount === 0) {
      await database.query(
        `DELETE FROM cerebro_feature_flags
          WHERE workspace_id = $1
            AND user_id = $2
            AND flag_key = $3`,
        [workspaceId, WORKSPACE_FLAG_USER, FEATURE_FLAG],
      );
    } else {
      await database.query(
        `UPDATE cerebro_feature_flags
            SET enabled = $4, updated_at = NOW()
          WHERE workspace_id = $1
            AND user_id = $2
            AND flag_key = $3`,
        [
          workspaceId,
          WORKSPACE_FLAG_USER,
          FEATURE_FLAG,
          previousFeatureFlag.rows[0]!.enabled,
        ],
      );
    }
    await database.end();
    await api.cleanup();
  }
});
