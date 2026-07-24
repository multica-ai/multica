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
  // This single contract intentionally drives the complete service-token
  // lifecycle. A loaded CI worker can spend close to three minutes before the
  // final disable/re-enable/revoke assertions, so leave enough time for those
  // assertions to use their own bounded waits.
  test.setTimeout(300_000);
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
  const killSwitchTokenName = `FIR-3755 kill switch ${suffix}`;
  const forbiddenIssueTitle = `FIR-3755 forbidden write ${suffix}`;
  const readableIssue = await api.createIssue(`FIR-3755 readable ${suffix}`);
  let rawToken = "";

  try {
    await api.setWorkspaceFeatureFlag(FEATURE_FLAG, true);
    const slug = await loginAsDefault(page);
    // loginAsDefault proves the real login and Issues surfaces, but the Issues
    // board is still completing a large request fan-out when its first marker
    // appears. Move Settings into an isolated page so those requests cannot
    // race the feature-flag hydration or exhaust the Settings renderer.
    const browserContext = page.context();
    let settingsPage = await browserContext.newPage();
    await page.close();
    const initialFlagsResponse = settingsPage.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        response.url().includes(`/workspaces/${workspaceId}/feature-flags`) &&
        response.status() === 200,
      { timeout: FLAG_HYDRATION_TIMEOUT_MS },
    );
    const initialTokensResponse = settingsPage.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        new URL(response.url()).pathname === "/api/service-tokens" &&
        response.status() === 200,
      { timeout: FLAG_HYDRATION_TIMEOUT_MS },
    );
    await settingsPage.goto(`/${slug}/settings?tab=tokens`, {
      waitUntil: "domcontentloaded",
    });
    await expect(settingsPage).toHaveURL(
      new RegExp(`/${slug}/settings\\?tab=tokens$`),
    );
    const initialFlagsPayload = (await (await initialFlagsResponse).json()) as {
      workspace_overrides?: Record<string, boolean>;
    };
    expect(initialFlagsPayload.workspace_overrides?.[FEATURE_FLAG]).toBe(true);
    await initialTokensResponse;

    // The settings shell performs a fresh authenticated bootstrap after the
    // direct route load. CI can take more than Playwright's default five
    // seconds to hydrate the workspace feature flags. Wait for both gated API
    // responses above before resolving the rendered section so route suspense
    // cannot be mistaken for a disabled feature.
    const serviceTokensHeading = settingsPage.getByRole("heading", {
      name: "Service tokens",
      exact: true,
    });
    await serviceTokensHeading.waitFor({
      state: "visible",
      timeout: FLAG_HYDRATION_TIMEOUT_MS,
    });
    let serviceTokensSection = serviceTokensHeading.locator(
      "xpath=ancestor::section",
    );
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
        settingsPage.getByRole("option", { name: option, exact: true }),
      ).toHaveCount(1);
    }
    await settingsPage
      .getByRole("option", { name: "30 days", exact: true })
      .click();
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

    const createdDialog = settingsPage.getByRole("dialog").filter({
      has: settingsPage.getByRole("heading", {
        name: "Service token created",
        exact: true,
      }),
    });
    await expect(createdDialog).toBeVisible();
    rawToken = (await createdDialog.locator("code").textContent())?.trim() ?? "";
    expect(rawToken).toMatch(/^msv_[a-f0-9]{40}$/);
    await createdDialog.getByRole("button", { name: "Done", exact: true }).click();
    await expect(createdDialog).toBeHidden();
    await expect(settingsPage.getByText(rawToken, { exact: true })).toHaveCount(
      0,
    );
    await settingsPage.close();
    settingsPage = await browserContext.newPage();
    const persistedFlagsResponse = settingsPage.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        response.url().includes(`/workspaces/${workspaceId}/feature-flags`) &&
        response.status() === 200,
      { timeout: FLAG_HYDRATION_TIMEOUT_MS },
    );
    const persistedTokensResponse = settingsPage.waitForResponse(
      (response) =>
        response.request().method() === "GET" &&
        new URL(response.url()).pathname === "/api/service-tokens" &&
        response.status() === 200,
      { timeout: FLAG_HYDRATION_TIMEOUT_MS },
    );
    await settingsPage.goto(`/${slug}/settings?tab=tokens`, {
      waitUntil: "domcontentloaded",
    });
    await persistedFlagsResponse;
    await persistedTokensResponse;
    const persistedServiceTokensHeading = settingsPage.getByRole("heading", {
      name: "Service tokens",
      exact: true,
    });
    await persistedServiceTokensHeading.waitFor({
      state: "visible",
      timeout: FLAG_HYDRATION_TIMEOUT_MS,
    });
    serviceTokensSection = persistedServiceTokensHeading.locator(
      "xpath=ancestor::section",
    );
    await expect(settingsPage.getByText(rawToken, { exact: true })).toHaveCount(
      0,
    );

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

    const revokeButton = tokenRow.getByRole("button", {
      name: `Revoke ${tokenName}`,
      exact: true,
    });
    await expect(revokeButton).toBeVisible({
      timeout: FLAG_HYDRATION_TIMEOUT_MS,
    });
    // The icon button is wrapped by Base UI's tooltip trigger. In headless
    // Chromium the synthetic pointer action can remain in tooltip
    // actionability even though the button is visible. Activate the same
    // accessible button through its keyboard contract so the test still
    // drives the real confirmation and revoke flow.
    await revokeButton.press("Enter");
    const revokeDialog = settingsPage.getByRole("alertdialog").filter({
      has: settingsPage.getByRole("heading", {
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
      await settingsPage.setViewportSize({
        width: evidence.width,
        height: evidence.height,
      });
      const screenshot = await settingsPage.screenshot({
        fullPage: true,
        path: test.info().outputPath(`${evidence.name}.png`),
      });
      await test.info().attach(evidence.name, {
        body: screenshot,
        contentType: "image/png",
      });
    }

    // Prove the workspace kill switch with a second, still-live token. The UI
    // visibility contract is covered by ServiceTokensWorkspaceGate's focused
    // component test; keeping flag transitions out of this already long browser
    // lifecycle avoids a Chromium renderer crash while preserving the real
    // management, auth, and audit contracts end to end.
    const killSwitchCreate = await fetch(`${API_BASE}/api/service-tokens`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${api.getToken()}`,
        "Content-Type": "application/json",
        "X-Workspace-ID": workspaceId,
      },
      body: JSON.stringify({
        name: killSwitchTokenName,
        scopes: ["issues:read"],
        expires_in_days: 30,
      }),
    });
    expect(killSwitchCreate.status).toBe(201);
    const killSwitchCreated = (await killSwitchCreate.json()) as {
      token?: string;
    };
    expect(killSwitchCreated.token).toMatch(/^msv_[a-f0-9]{40}$/);
    const killSwitchRawToken = killSwitchCreated.token!;
    const killSwitchRecord = await database.query<{ id: string }>(
      `SELECT id::text
         FROM cerebro_service_token
        WHERE workspace_id = $1
          AND name = $2`,
      [workspaceId, killSwitchTokenName],
    );
    expect(killSwitchRecord.rowCount).toBe(1);
    const killSwitchTokenId = killSwitchRecord.rows[0]!.id;

    const enabledKillSwitchRead = await fetch(
      `${API_BASE}/api/service/issues`,
      { headers: { Authorization: `Bearer ${killSwitchRawToken}` } },
    );
    expect(enabledKillSwitchRead.status).toBe(200);
    const enabledKillSwitchAudits = await database.query<{ count: string }>(
      `SELECT COUNT(*)::text AS count
         FROM cerebro_service_token_audit
        WHERE service_token_id = $1
          AND event = 'used'`,
      [killSwitchTokenId],
    );
    expect(Number(enabledKillSwitchAudits.rows[0]?.count ?? "0")).toBe(1);

    await api.setWorkspaceFeatureFlag(FEATURE_FLAG, false);
    const disabledRead = await fetch(`${API_BASE}/api/service/issues`, {
      headers: { Authorization: `Bearer ${killSwitchRawToken}` },
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
      [killSwitchTokenId],
    );
    expect(Number(disabledAuditCount.rows[0]?.count ?? "0")).toBe(1);
  } finally {
    await database.query(
      `DELETE FROM cerebro_service_token
        WHERE workspace_id = $1
          AND name = ANY($2::text[])`,
      [workspaceId, [tokenName, killSwitchTokenName]],
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
