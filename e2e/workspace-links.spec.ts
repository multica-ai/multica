import "./env";
import { expect, test } from "@playwright/test";
import { createTestApi } from "./helpers";

test("configures workspace and project home links and restores issues when cleared", async ({ page }) => {
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
    // A deterministic external document keeps review screenshots independent
    // of example.com's availability and framing policy. The API remains real.
    await page.route("https://example.com/**", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "text/html; charset=utf-8",
        body: `<!doctype html>
          <html lang="en">
            <head>
              <meta charset="utf-8">
              <title>Team documentation</title>
              <style>
                body { margin: 0; padding: 48px; font: 16px/1.6 system-ui; color: #243047; background: #f5f7fb; }
                main { max-width: 760px; margin: auto; padding: 40px; background: white; border-radius: 12px; }
                h1 { margin-top: 0; color: #18223a; }
                h2 { margin-top: 28px; font-size: 20px; }
                small { color: #64748b; }
              </style>
            </head>
            <body>
              <main>
                <small>QUICK LINKS REVIEW FIXTURE</small>
                <h1>Team documentation</h1>
                <p>Shared guides for the tools and workflows our team uses every day.</p>
                <h2>Getting started</h2>
                <ul>
                  <li>Read the project overview and onboarding checklist.</li>
                  <li>Find dashboards, release notes, and deployment guides.</li>
                  <li>Contact the team when you need help.</li>
                </ul>
              </main>
            </body>
          </html>`,
      });
    });
    await page.setViewportSize({ width: 1440, height: 960 });
    await page.goto(`/${workspace.slug}/settings?tab=links`);
    await page.getByRole("button", { name: "Add link" }).click();
    await page.getByRole("textbox", { name: "Name", exact: true }).fill("Team documentation");
    await page.getByLabel("Show in header").selectOption("icon");
    await page.getByRole("textbox", { name: "URL", exact: true }).fill(
      "https://example.com/{workspace_slug}?workspace={workspace_id}",
    );
    await expect(
      page.getByText(`https://example.com/${workspace.slug}?workspace=${workspace.id}`, { exact: true }),
    ).toBeVisible();
    await page.getByRole("radio", { name: "Embedded, as home page", exact: true }).check();
    if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/home-editor.png` });
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByText("Team documentation", { exact: true })).toBeVisible();
    if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/workspace-links.png` });
    await page.goto(`/${workspace.slug}/issues`);
    const iconOnly = page
      .getByRole("navigation", { name: "Quick links" })
      .getByRole("link", { name: "Team documentation" });
    await expect(iconOnly).toHaveAccessibleName("Team documentation");
    await expect(iconOnly).not.toContainText("Team documentation");
    await page.goto(`/${workspace.slug}/start`);
    await expect(page).toHaveURL(new RegExp(`/${workspace.slug}/links/`));
    await expect(page.locator("iframe")).toHaveAttribute(
      "src",
      `https://example.com/${workspace.slug}?workspace=${workspace.id}`,
    );
    await expect(page.locator("iframe")).not.toHaveAttribute("sandbox", /allow-same-origin/);
    await expect(page.getByRole("button", { name: "Open in new tab" }).last()).toBeVisible();
    await expect(
      page.frameLocator("iframe").getByRole("heading", { name: "Team documentation", exact: true }),
    ).toBeVisible();
    if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/workspace-home-landing.png` });
    await page.goto(`/${workspace.slug}/projects/${project.id}`);
    await page.getByRole("button", { name: "Add link", exact: true }).last().click();
    await page.getByRole("textbox", { name: "Name", exact: true }).fill("Project dashboard");
    await page.getByRole("textbox", { name: "URL", exact: true }).fill("https://example.com/projects/{project_id}");
    await page.getByRole("radio", { name: "Embedded, as home page", exact: true }).check();
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await page.goto(`/${workspace.slug}/projects/${project.id}`);
    await expect(page.getByRole("button", { name: "Home", exact: true })).toHaveAttribute("aria-pressed", "true");
    await expect(page.locator("iframe")).toHaveAttribute("src", `https://example.com/projects/${project.id}`);
    await expect(page.frameLocator("iframe").getByRole("heading", { name: "Team documentation", exact: true })).toBeVisible();
    if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/project-home.png` });
    await page.getByRole("button", { name: "Issues", exact: true }).click();
    await expect(page.locator("iframe")).toHaveCount(0);
    const header = page.getByRole("navigation", { name: "Quick links" });
    await expect(header.getByRole("link", { name: "Team documentation" })).not.toContainText("Team documentation");
    await expect(header.getByRole("link", { name: "Project dashboard" })).toContainText("Project dashboard");
    if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/header-icon-only.png` });
    await expect(page.getByRole("link", { name: "Project dashboard", exact: true })).toBeVisible();

    await page.goto(`/${workspace.slug}/settings?tab=links`);
    await page.getByRole("button", { name: "Edit link", exact: true }).click();
    await page.getByRole("radio", { name: "Embedded", exact: true }).check();
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await page.goto(`/${workspace.slug}/start`);
    await expect(page).toHaveURL(new RegExp(`/${workspace.slug}/issues$`));
    await page.goto(`/${workspace.slug}/issues`);
    await page.setViewportSize({ width: 480, height: 900 });
    await page.getByRole("button", { name: "Quick links", exact: true }).click();
    await expect(page.getByRole("menuitem", { name: "Team documentation" })).toBeVisible();
    if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/narrow-links.png` });

    await page.setViewportSize({ width: 1440, height: 960 });
    await page.goto(`/${workspace.slug}/settings?tab=preferences`);
    await page.getByRole("combobox", { name: "Language", exact: true }).click();
    await page.getByRole("option", { name: "简体中文", exact: true }).click();
    await expect(page.getByRole("combobox", { name: "语言", exact: true })).toBeVisible();
    await page.goto(`/${workspace.slug}/settings?tab=links`);
    await expect(page.getByRole("button", { name: "添加链接", exact: true })).toBeVisible();
    await expect(page.getByText("Team documentation", { exact: true })).toBeVisible();
    expect(
      (await page.context().cookies()).find((cookie) => cookie.name === "multica-locale")?.value,
    ).toBe("zh-Hans");
    if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/settings-links-zh.png` });
    await page.getByRole("button", { name: "编辑链接", exact: true }).click();
    await page.getByRole("radio", { name: "内嵌打开并设为首页", exact: true }).check();
    if (screenshotDir) await page.screenshot({ path: `${screenshotDir}/home-editor-zh.png` });
    await page.getByRole("button", { name: "取消", exact: true }).click();
  } finally {
    for (const link of await api.getLinks()) await api.deleteLink(link.id);
    await api.cleanup();
  }
});
