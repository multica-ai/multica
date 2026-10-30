import { expect, test } from "@playwright/test";
import { TestApiClient } from "./fixtures";

for (const type of ["select", "multi_select"] as const) {
  test(`edits and selects from 200 ${type} options`, async ({ page }) => {
    const api = new TestApiClient();
    const suffix = Date.now().toString(36);
    let workspaceCreated = false;
    try {
      await api.login(`select-options-${type}-${suffix}@multica.ai`, "Property owner");
      const workspace = await api.ensureWorkspace("Select options", `options-${type === "select" ? "s" : "m"}-${suffix}`);
      workspaceCreated = true;
      await api.markUserOnboarded();
      const property = await api.createProperty("Services", type, {
        options: Array.from({ length: 200 }, (_, i) => ({ name: `Service ${i + 1}`, color: "#3b82f6" })),
      });
      const options = property.config.options!;
      const issue = await api.createIssue("Choose a service", {
        properties: { [property.id]: type === "select" ? options[0]!.id : [options[0]!.id] },
      });
      const token = api.getToken();
      if (!token) throw new Error("Owner login did not return a token");
      await page.addInitScript((value) => {
        localStorage.setItem("multica_token", value);
        localStorage.setItem("multica:chat:isOpen", "false");
      }, token);
      await page.setViewportSize({ width: 1280, height: 800 });
      await page.goto(`/${workspace.slug}/settings?tab=properties`);
      await page.getByRole("button", { name: "Actions for Services", exact: true }).click();
      await page.getByRole("menuitem", { name: "Edit", exact: true }).click();
      const dialog = page.getByRole("dialog", { name: "Edit property" });
      await expect(dialog.getByPlaceholder("Option name")).toHaveCount(200);
      await expect(dialog.getByRole("button", { name: "Add option", exact: true })).toBeDisabled();
      await dialog.getByPlaceholder("Option name").last().fill("Service 200 updated");
      await expect(dialog.getByRole("button", { name: "Save property" })).toBeInViewport();
      const editorImage = test.info().outputPath(`${type}-editor.png`);
      await page.screenshot({ path: editorImage });
      await test.info().attach("200-option editor", { path: editorImage, contentType: "image/png" });
      const updated = page.waitForResponse((response) =>
        response.url().endsWith(`/api/properties/${property.id}`) && response.request().method() === "PATCH",
      );
      await dialog.getByRole("button", { name: "Save property" }).click();
      expect((await updated).ok()).toBe(true);
      await expect(dialog).not.toBeVisible();

      await page.goto(`/${workspace.slug}/issues/${issue.identifier}`);
      await page.locator("[data-right-sidebar-panel]").getByRole("button", { name: "Service 1", exact: true }).click();
      const search = page.getByRole("textbox", { name: "Filter options" });
      await search.fill("service 200");
      await expect(page.getByRole("button", { name: "Service 200 updated", exact: true })).toBeVisible();
      await expect(page.locator("button[data-picker-item]").filter({ hasText: /^Service 1$/ })).toHaveCount(0);
      const searchImage = test.info().outputPath(`${type}-search.png`);
      await page.screenshot({ path: searchImage });
      await test.info().attach("Search last option", { path: searchImage, contentType: "image/png" });
      const saved = page.waitForResponse((response) =>
        response.url().endsWith(`/api/issues/${issue.id}/properties/${property.id}`)
        && response.request().method() === "PUT",
      );
      await search.press("Enter");
      expect((await saved).ok()).toBe(true);
      await page.reload();
      await expect(page.locator("[data-right-sidebar-panel]").getByText("Service 200 updated", { exact: true })).toBeVisible();
    } finally {
      await api.cleanup();
      if (workspaceCreated) await api.deleteWorkspace();
    }
  });
}
