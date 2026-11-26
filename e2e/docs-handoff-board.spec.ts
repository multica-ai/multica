import { expect, test } from "@playwright/test";
import { API_BASE, TestApiClient } from "./fixtures";

test("a Docs handoff survives reload, opens its issue and stays within its workspace", async ({ page }) => {
  const config = await fetch(`${API_BASE}/api/config`);
  expect(config.ok).toBeTruthy();
  test.skip(!(await config.json()).feature_flags?.docs_v1, "Start the API with FF_DOCS_V1=true.");

  const team = new TestApiClient();
  const otherTeam = new TestApiClient();
  const suffix = `${Date.now()}-${test.info().workerIndex}`;
  const title = "Review launch brief";
  const otherTitle = "Review packaging brief";
  try {
    await team.login(`handoff-${suffix}@example.test`, "Alex Morgan");
    const workspace = await team.ensureWorkspace("Launch team", `launch-${suffix}`);
    await team.markUserOnboarded();
    await team.createDoc("Launch brief", "Send the approved brief to the launch reviewer.");
    await team.createIssue(title);
    const { issues } = await team.searchIssues(title);
    const issue = issues.find((item) => item.title === title)!;
    await otherTeam.login(`packaging-${suffix}@example.test`, "Sam Taylor");
    await otherTeam.ensureWorkspace("Packaging team", `packaging-${suffix}`);
    await otherTeam.createIssue(otherTitle);

    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, team.getToken());
    const docs = `/${workspace.slug}/docs`;
    await page.goto(docs);
    await page.getByRole("button", { name: "New board", exact: true }).click();
    await page.getByRole("button", { name: "Insert handoff" }).click();
    await page.getByRole("button", { name: "Link issue", exact: true }).click();
    const picker = page.getByRole("dialog");
    await picker.getByRole("combobox").fill("Review");
    const option = picker.getByRole("option").filter({ hasText: title });
    await expect(option).toBeVisible();
    await expect(picker.getByRole("option").filter({ hasText: otherTitle })).toHaveCount(0);
    await option.click();

    const boardId = new URL(page.url()).pathname.split("/").pop()!;
    const saved = page.waitForResponse((r) =>
      r.request().method() === "PATCH" && r.url().endsWith(`/api/docs/${boardId}`),
    );
    await page.getByRole("button", { name: "Save board" }).click();
    expect((await saved).ok()).toBeTruthy();
    const board = await team.getDoc(boardId);
    expect(board.content).toContain("Prepare brief");
    expect(board.content).toContain("Review brief");
    expect(board.content).toContain(issue.identifier);
    await expect(otherTeam.getDoc(boardId)).rejects.toThrow("get doc: 404");

    await page.goto(docs);
    await page.reload();
    await page.getByRole("complementary").getByRole("link", { name: board.title, exact: true }).click();
    const link = page.getByRole("link", { name: issue.identifier, exact: true });
    await expect(link).toHaveAttribute("href", `/${workspace.slug}/issues/${issue.identifier}`);
    await link.click();
    await expect(page).toHaveURL(new RegExp(`/issues/${issue.identifier}$`), { timeout: 30_000 });
    await expect(page.getByText(title, { exact: true }).first()).toBeVisible();
  } finally {
    for (const client of [team, otherTeam]) {
      await client.cleanup();
      await client.deleteWorkspace();
    }
  }
});
