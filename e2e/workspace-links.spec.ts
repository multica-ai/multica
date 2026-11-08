import "./env";
import { expect, test } from "@playwright/test";
import { createTestApi } from "./helpers";

test("configures workspace and project quick links and opens the embedded page", async ({ page }) => {
  test.setTimeout(180_000);
  const api = await createTestApi();
  const workspace = (await api.getWorkspaces())[0]!;
  api.setWorkspaceId(workspace.id);
  api.setWorkspaceSlug(workspace.slug);
  const project = await api.createProject("Quick links project");
  const screenshotDir = process.env.LINKS_SCREENSHOT_DIR;
  try {
    const token = api.getToken();
    if (!token) throw new Error("E2E login failed");
    await page.addInitScript((authToken) => {
      localStorage.setItem("multica_token", authToken);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, token);
    await page.setViewportSize({ width: 1440, height: 960 });
    await page.goto(`/${workspace.slug}/settings?tab=links`);
    await page.getByRole("button", { name: "Add link" }).click();
    await page.getByRole("textbox", { name: "Name", exact: true }).fill("Team documentation");
    await page.getByRole("textbox", { name: "URL", exact: true }).fill("https://example.com/{workspace_slug}?workspace={workspace_id}");
    await expect(page.getByText(`https://example.com/${workspace.slug}?workspace=${workspace.id}`, { exact: true })).toBeVisible();
    if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/link-editor.png` });
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByText("Team documentation", { exact: true })).toBeVisible();
    if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/workspace-links.png` });
    await page.goto(`/${workspace.slug}/issues`);
    await page.getByRole("link", { name: "Team documentation" }).click();
    await expect(page.locator("iframe")).toHaveAttribute("src", `https://example.com/${workspace.slug}?workspace=${workspace.id}`);
    await expect(page.locator("iframe")).not.toHaveAttribute("sandbox", /allow-same-origin/);
    await expect(page.getByRole("button", { name: "Open in new tab" }).last()).toBeVisible();
    if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/embedded-link.png` });
    await page.goto(`/${workspace.slug}/projects/${project.id}`);
    await page.getByRole("button", { name: "Add link", exact: true }).last().click();
    await page.getByRole("textbox", { name: "Name", exact: true }).fill("Project dashboard");
    await page.getByRole("textbox", { name: "URL", exact: true }).fill("https://example.com/projects/{project_id}");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByRole("link", { name: "Project dashboard" })).toBeVisible();
    if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/project-links.png` });
    await page.getByRole("link", { name: "Project dashboard" }).click();
    await expect(page.locator("iframe")).toHaveAttribute("src", `https://example.com/projects/${project.id}`);
    await page.goto(`/${workspace.slug}/issues`);
    await page.setViewportSize({ width: 480, height: 900 });
    await page.getByRole("button", { name: "Quick links", exact: true }).click();
    await expect(page.getByRole("menuitem", { name: "Team documentation" })).toBeVisible();
    if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/narrow-links.png` });
  } finally {
    for (const link of await api.getLinks()) await api.deleteLink(link.id);
    await api.cleanup();
  }
});
