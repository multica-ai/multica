import { expect, test } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";

for (const width of [360, 390, 1440]) {
  test(`project tabs leave the sidebar toggle reachable at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 844 });
    const api = await createTestApi();
    try {
      const project = await api.createProject("Project with a long title for header navigation");
      const slug = await loginAsDefault(page);
      await page.goto(`/${slug}/projects/${project.id}`, { waitUntil: "domcontentloaded" });
      for (const tab of ["Overview", "Activity"]) {
        await page.getByRole("tab", { name: tab, exact: true }).click();
        await expect(page.getByRole("tab", { name: tab, exact: true })).toHaveAttribute("aria-selected", "true");
        const toggle = page.getByRole("button", { name: "Toggle right sidebar", exact: true });
        await toggle.click({ timeout: 5000 });
        if (width < 768) {
          await expect(page.getByRole("dialog")).toBeVisible();
          await page.keyboard.press("Escape");
          await expect(page.getByRole("dialog")).toBeHidden();
        } else {
          await toggle.click();
        }
      }
    } finally {
      await api.cleanup();
    }
  });
}
