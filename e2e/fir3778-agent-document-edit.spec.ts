import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

// FIR-3778 — a person asks an agent to write a document for them, then edits it
// themselves. Before the fix the document opened read-only: no edit action, no
// cursor in the text.
//
// Each of the three gates this exercises also has its own test
// (server/internal/handler/artifact_requester_cerebro_test.go,
// server/internal/cerebro/note/fir3778_agent_document_edit_db_test.go, and
// packages/cerebro-artifacts/views/pages/document-view-page.test.tsx); this spec
// is the whole-path proof on top of them, run by the Cerebro E2E workflow.
test.describe("FIR-3778 — editing a document an agent created for you", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.cleanup();
  });

  test("the requester can type in the document and the change survives a reload", async ({
    page,
  }) => {
    const doc = await api.createAgentDocument(
      "Agent checklist " + Date.now(),
      "first line",
    );

    await page.goto(`/${api.getWorkspaceSlug()}/documents/${doc.id}`);

    // The editable path renders the inline editor; the read-only path does not.
    const editor = page.locator('[contenteditable="true"]').first();
    await expect(editor).toBeVisible({ timeout: 15000 });

    const addition = " edited by the requester";
    await editor.click();
    await page.keyboard.press("End");
    await page.keyboard.type(addition);

    // Autosave is debounced — wait for the status line, never a fixed timer.
    await expect(page.getByText("Saved")).toBeVisible({ timeout: 15000 });

    await page.reload();
    await expect(page.locator('[contenteditable="true"]').first()).toContainText(
      addition,
      { timeout: 15000 },
    );
  });
});
