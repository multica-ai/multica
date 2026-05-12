import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

const API_BASE =
  process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;

// The workflow engine is gated by CEREBRO_WORKFLOWS_ENABLED on the server
// side. When that env var is missing on the test runner's backend, the
// engine's bus listener no-ops and the create_sub_issue side-effect won't
// happen — so the assertion would flake. Skip cleanly with a clear reason.
const ENGINE_ENABLED = ["1", "true", "yes", "on"].includes(
  (process.env.CEREBRO_WORKFLOWS_ENABLED ?? "").toLowerCase().trim(),
);

test.describe("Cerebro workflows (JEH-1047)", () => {
  let api: TestApiClient;
  let token: string;
  let workspaceSlug: string;

  test.beforeEach(async ({ page }) => {
    test.skip(!ENGINE_ENABLED, "CEREBRO_WORKFLOWS_ENABLED not set on the backend.");
    api = await createTestApi();
    token = api.getToken() ?? "";
    expect(token).not.toBe("");
    // Enable the per-user UI flag so the workflows page renders and the
    // sidebar entry shows up. Server-side execution stays gated by the
    // env var checked above.
    await setFeatureFlag(token, "cerebro_workflows", true);
    workspaceSlug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
      await setFeatureFlag(token, "cerebro_workflows", false).catch(() => {});
    }
  });

  test("Done når: template → status_changed → create_sub_issue → run logged", async ({
    page,
  }) => {
    // ---- Step 0: a project + a target issue exist ----
    const project = await api.createProject(
      `WF E2E Project ${Date.now()}`,
    );
    const parentIssue = await api.createIssue(`WF E2E Parent ${Date.now()}`, {
      project_id: project.id,
      status: "todo",
    });

    // ---- Step 1: open the workflows page from the sidebar ----
    await page.goto(`/${workspaceSlug}/workflows`, { waitUntil: "domcontentloaded" });
    await expect(page.getByRole("heading", { name: "Workflows" })).toBeVisible({
      timeout: 10_000,
    });

    // ---- Step 2: open the form, click "Brug" on the status-change template ----
    await page.getByRole("button", { name: "Nyt workflow" }).click();
    await expect(page.getByRole("heading", { name: "Nyt workflow" })).toBeVisible();

    // The template picker uses data-testid="template-use-<key>" so the
    // assertion targets the right row regardless of label wording changes.
    await page.getByTestId("template-use-status-change").click();

    // After applying the template, the form's "Til status" field should
    // hold "in_review". That's the canary — if it's wrong, the template
    // pre-fill regressed.
    await expect(page.getByLabel("Til status")).toHaveValue("in_review");

    // ---- Step 3: save the workflow ----
    await page.getByRole("button", { name: "Gem" }).click();

    // Back on the list page. The new workflow name (template default) shows.
    await page.waitForURL(`**/workflows`, { timeout: 10_000 });
    await expect(page.getByText("Auto QA on in_review")).toBeVisible();

    // ---- Step 4: move the parent issue to in_review ----
    // This fires the issue:updated bus event with status_changed=true and
    // prev_status="todo". The engine subscribes on the same in-process
    // bus, so by the time this fetch returns the action has run.
    await updateIssueStatus(token, parentIssue.id, "in_review");

    // ---- Step 5: a sub-issue exists under the parent ----
    // The bus is synchronous but the DB write that creates the sub-issue
    // happens on a separate connection — poll briefly to absorb that lag.
    const child = await pollForChild(token, parentIssue.id);
    expect(child, "expected one sub-issue to be auto-created").toBeDefined();
    expect(child!.title).toContain(parentIssue.title);

    // ---- Step 6: workflow_run is success ----
    const runs = await listWorkflowRuns(token);
    const matching = runs.find(
      (r) => r.target_issue_id === parentIssue.id && r.status === "success",
    );
    expect(matching, `expected a success run for issue ${parentIssue.id}`).toBeDefined();

    // ---- Step 7: the run shows up in the log page ----
    await page.goto(`/${workspaceSlug}/workflows/runs`, { waitUntil: "domcontentloaded" });
    await expect(page.getByRole("heading", { name: "Workflow-log" })).toBeVisible();
    await expect(page.getByText("success").first()).toBeVisible({ timeout: 10_000 });

    // Cleanup of the workflow itself; api.cleanup() only knows about
    // issue/project/inbox cleanup.
    await deleteCreatedWorkflows(token);
    // Cleanup the auto-created sub-issue so api.cleanup doesn't orphan it.
    await deleteIssue(token, child!.id);
  });
});

// ---- helpers (local to this spec — no need to bloat TestApiClient) ----

async function setFeatureFlag(token: string, key: string, enabled: boolean) {
  // The feature-flag route is keyed on workspace UUID, not slug, so we
  // resolve the id from the workspaces list rather than relying on the
  // header-based fallback.
  const res = await fetch(`${API_BASE}/api/workspaces`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  const data = (await res.json()) as {
    workspaces?: Array<{ slug?: string; id?: string }>;
  };
  const ws = (data.workspaces ?? []).find((w) => !!w.id);
  if (!ws?.id) throw new Error("could not resolve workspace id");
  const r = await fetch(`${API_BASE}/api/workspaces/${ws.id}/feature-flags/${key}`, {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({ enabled }),
  });
  if (!r.ok && r.status !== 204) {
    throw new Error(`setFeatureFlag failed: ${r.status} ${await r.text()}`);
  }
}

async function updateIssueStatus(token: string, id: string, status: string) {
  const res = await fetch(`${API_BASE}/api/issues/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify({ status }),
  });
  if (!res.ok) {
    throw new Error(`updateIssueStatus failed: ${res.status} ${await res.text()}`);
  }
}

async function pollForChild(token: string, parentId: string) {
  // The workflow_run + create_sub_issue path is synchronous on the bus, but
  // the http response writer returns before all bus subscribers finish in
  // some go-chi configurations — give the listener up to ~2 s.
  for (let i = 0; i < 20; i++) {
    const res = await fetch(`${API_BASE}/api/issues/${parentId}/children`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (res.ok) {
      const data = (await res.json()) as { issues?: Array<{ id: string; title: string }> };
      const issues = data.issues ?? [];
      if (issues.length > 0) return issues[0];
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  return undefined;
}

async function listWorkflowRuns(token: string) {
  const res = await fetch(`${API_BASE}/api/cerebro/workflows/runs?limit=20`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) throw new Error(`listWorkflowRuns failed: ${res.status}`);
  const data = (await res.json()) as {
    runs?: Array<{ id: string; target_issue_id?: string; status: string }>;
  };
  return data.runs ?? [];
}

async function deleteCreatedWorkflows(token: string) {
  const res = await fetch(`${API_BASE}/api/cerebro/workflows`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) return;
  const data = (await res.json()) as { workflows?: Array<{ id: string; name: string }> };
  for (const wf of data.workflows ?? []) {
    if (wf.name.startsWith("Auto QA on in_review")) {
      await fetch(`${API_BASE}/api/cerebro/workflows/${wf.id}`, {
        method: "DELETE",
        headers: { Authorization: `Bearer ${token}` },
      });
    }
  }
}

async function deleteIssue(token: string, id: string) {
  await fetch(`${API_BASE}/api/issues/${id}`, {
    method: "DELETE",
    headers: { Authorization: `Bearer ${token}` },
  });
}

