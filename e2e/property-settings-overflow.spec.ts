import { expect, test } from "@playwright/test";
import { TestApiClient } from "./fixtures";

// #9639: viewport-based columns clipped the action menu when the settings
// content was narrower than the table, even though the admin could create.
test("admins can reach property actions in narrow settings and save edits", async ({ page }) => {
  const owner = new TestApiClient();
  const admin = new TestApiClient();
  const suffix = `${Date.now().toString(36)}-${test.info().workerIndex}`;
  const email = `property-admin-${suffix}@multica.ai`;
  let workspaceCreated = false;
  try {
    await owner.login(`property-owner-${suffix}@multica.ai`, "Property owner");
    const workspace = await owner.ensureWorkspace("Property settings", `properties-${suffix}`);
    workspaceCreated = true;
    await admin.login(email, "Property admin");
    await owner.addMemberByEmail(email, "admin");
    admin.setWorkspaceId(workspace.id);
    admin.setWorkspaceSlug(workspace.slug);
    await admin.markUserOnboarded();
    const name = `Segment ${suffix}`;
    const property = await admin.createProperty(name);
    const token = admin.getToken();
    if (!token) throw new Error("Admin login did not return a token");
    await page.addInitScript((value) => {
      localStorage.setItem("multica_token", value);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, token);
    await page.setViewportSize({ width: 1024, height: 768 });
    await page.goto(`/${workspace.slug}/settings?tab=properties`);
    const actions = page.getByRole("button", { name: `Actions for ${name}`, exact: true });
    for (const width of [1024, 1280, 1600]) {
      await page.setViewportSize({ width, height: 768 });
      await expect(actions).toBeVisible();
      // Visibility alone passes for a button clipped by overflow:hidden.
      // Check actual pointer targeting before focus/scroll can mask clipping.
      await expect.poll(() => actions.evaluate((element) => {
        const box = element.getBoundingClientRect();
        return element.contains(document.elementFromPoint(
          box.x + box.width / 2, box.y + box.height / 2,
        ));
      })).toBe(true);
    }
    await page.setViewportSize({ width: 1024, height: 768 });
    await actions.focus();
    await page.keyboard.press("Enter");
    await page.getByRole("menuitem", { name: "Edit", exact: true }).focus();
    await page.keyboard.press("Enter");
    const dialog = page.getByRole("dialog", { name: "Edit property" });
    await expect(dialog).toBeVisible();
    const renamed = `${name} updated`;
    await dialog.getByLabel("Name", { exact: true }).fill(renamed);
    await expect(dialog.getByLabel("Name", { exact: true })).toHaveValue(renamed);
    const saved = page.waitForResponse((response) =>
      response.url().endsWith(`/api/properties/${property.id}`)
      && response.request().method() === "PATCH",
    );
    await dialog.getByRole("button", { name: "Save property" }).click();
    expect((await saved).ok()).toBe(true);
    await expect(dialog).not.toBeVisible();
    await page.reload();
    await expect(page.getByRole("button", { name: `Actions for ${renamed}`, exact: true })).toBeVisible();
  } finally {
    await admin.cleanup();
    if (workspaceCreated) await owner.deleteWorkspace();
  }
});
