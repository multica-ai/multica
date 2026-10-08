import { test, expect } from "@playwright/test";
import { loginAsDefault, waitForPageText } from "./helpers";

const REPO = "https://github.com/multica-ai/multica";

/**
 * The checkout ref of a github_repo project resource, end to end.
 *
 * Covers what only the full stack can show: the ref survives project creation,
 * comes back on the project page, and an edit persists — the server replaces
 * resource_ref wholesale rather than deep-merging, so a payload that drops the
 * URL is a class of bug the component tests alone cannot see.
 */
test("pins, edits and clears a repository's checkout ref", async ({ page }) => {
  const slug = await loginAsDefault(page);

  await page.goto(`/${slug}/projects`, { waitUntil: "domcontentloaded" });
  await waitForPageText(page, "Projects");
  await page.getByRole("button", { name: /new project/i }).first().click();

  // TitleEditor is a contenteditable, not an <input> with a placeholder attr.
  await page.getByRole("textbox", { name: /project title/i }).fill("Release line");
  await page.getByRole("button", { name: /repos/i }).first().click();
  await page.getByPlaceholder(/github\.com\/owner\/repo/i).fill(REPO);
  await page.getByLabel(/starting branch/i).fill("release/2026-09");
  await page.getByRole("button", { name: /^add$/i }).click();
  await page.getByRole("button", { name: /^create project$/i }).click();

  await waitForPageText(page, "Release line");
  await expect(page.getByText("release/2026-09")).toBeVisible({ timeout: 15000 });

  // Editing an attached resource — the affordance the UI never had.
  await page.getByTitle(/change the branch tasks work on/i).first().click();
  await expect(page.getByText(/which branch should tasks work on/i)).toBeVisible();

  // A ref git could not resolve is refused before it is stored, rather than
  // failing minutes later inside a task with a repo-cache error.
  await page.getByLabel(/starting branch/i).fill("main..dev");
  await expect(page.getByRole("button", { name: /^save$/i })).toBeDisabled();

  await page.getByLabel(/starting branch/i).fill("v1.4.0");
  await page.getByRole("button", { name: /^save$/i }).click();
  await expect(page.getByText("v1.4.0")).toBeVisible({ timeout: 10000 });
  await expect(page.getByText("release/2026-09")).toHaveCount(0);

  // The saved value survives a reload — i.e. it reached the database, and the
  // URL rode along with it rather than being replaced away.
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect(page.getByText("v1.4.0")).toBeVisible({ timeout: 15000 });
  await expect(page.getByText("multica-ai/multica")).toBeVisible();

  // Clearing goes back to the repository's default branch.
  await page.getByTitle(/change the branch tasks work on/i).first().click();
  await page.getByLabel(/starting branch/i).fill("");
  await page.getByRole("button", { name: /^save$/i }).click();
  await expect(page.getByText("v1.4.0")).toHaveCount(0, { timeout: 10000 });
  await expect(page.getByText("multica-ai/multica")).toBeVisible();
  // Not an empty row: an unpinned repo says which branch it uses, so clearing
  // is confirmable rather than indistinguishable from the setting not existing.
  // Exact text, because the success toast also says "Back to the default branch".
  await expect(page.getByText("Default branch", { exact: true })).toBeVisible();
});
