import { test, expect, type Browser, type Page } from "@playwright/test";
// Live issue descriptions need collab and the issue_description_live flag:
// run `make up` with FF_ISSUE_DESCRIPTION_LIVE=true. Without them the issue
// page keeps its solo editor and this spec skips.
import { TestApiClient } from "./fixtures";

let alice: TestApiClient;
let bob: TestApiClient;
let slug: string;
test.beforeEach(async () => {
  const suffix = `${Date.now()}-${test.info().workerIndex}`;
  bob = new TestApiClient();
  await bob.login(`issue-live-bob-${suffix}@multica.ai`, "Bob Chen");
  await bob.markUserOnboarded();
  alice = new TestApiClient();
  await alice.login(`issue-live-alice-${suffix}@multica.ai`, "Alice Park");
  slug = (await alice.ensureWorkspace("Launch team", `issue-live-${suffix}`)).slug;
  await alice.markUserOnboarded();
  await alice.addMemberByEmail(bob.getEmail());
});
test.afterEach(async () => {
  await alice.cleanup();
  await alice.deleteWorkspace();
});

/** Opens the issue as `token` and waits for the live editor to take over from the solo one. */
async function openLive(browser: Browser, token: string, issueId: string) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await context.newPage();
  page.on("pageerror", (error) => {
    throw error;
  });
  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
    localStorage.setItem("multica:chat:isOpen", "false");
  }, token);
  const ticket = page.waitForResponse((response) => response.url().endsWith(`/api/issues/${issueId}/collab-ticket`));
  await page.goto(`/${slug}/issues/${issueId}`);
  const live = ((await (await ticket).json()) as { enabled?: boolean }).enabled === true;
  const editor = description(page);
  await expect(editor).toBeVisible();
  if (live) {
    // The solo editor unmounts once the live one is shown.
    await editor.evaluate((element) => element.setAttribute("data-e2e-solo", ""));
    await expect(page.locator("[data-e2e-solo]")).toHaveCount(0, { timeout: 15_000 });
  }
  return { page, live };
}

function description(page: Page) {
  return page.locator('[data-comment-content^="description:"] .ProseMirror[contenteditable="true"]:visible');
}

/** The description's text without collaborators' carets, one line per block. */
async function descriptionText(page: Page) {
  return description(page).evaluate((element) => {
    const copy = element.cloneNode(true) as HTMLElement;
    copy.querySelectorAll(".collaboration-carets__caret").forEach((caret) => caret.remove());
    return Array.from(copy.children, (block) => block.textContent ?? "").join("\n");
  });
}

async function typeAt(page: Page, anchor: string, key: "Home" | "End", text: string) {
  await description(page).getByText(anchor).click();
  await page.keyboard.press(key);
  await page.keyboard.type(text, { delay: 20 });
}

test("two people and an API edit share one live description", async ({ browser }, info) => {
  const issue = await alice.createIssue("Launch plan", {
    description: "Goals for the launch.\n\nOpen questions remain.",
  });
  const a = await openLive(browser, alice.getToken()!, issue.id);
  test.skip(!a.live, "Live issue descriptions are off; start collab with FF_ISSUE_DESCRIPTION_LIVE=true.");
  const b = await openLive(browser, bob.getToken()!, issue.id);

  // Both type at once, in different paragraphs; a status change and a
  // local edit through the API land while they are still typing.
  await Promise.all([
    typeAt(a.page, "Goals for the launch.", "End", " Ship on Friday."),
    typeAt(b.page, "Open questions remain.", "Home", "Bob: "),
    (async () => {
      await alice.updateIssue(issue.id, { status: "in_progress" });
      await alice.editIssueDescription(issue.id, "Open questions", "Two open questions");
    })(),
  ]);

  const expected = "Goals for the launch. Ship on Friday.\n\nBob: Two open questions remain.";
  for (const { page } of [a, b]) {
    await expect.poll(() => descriptionText(page)).toBe(expected.replace("\n\n", "\n"));
  }
  await expect(a.page.locator(".collaboration-carets__label", { hasText: "Bob Chen" })).toBeVisible();
  await expect
    .poll(async () => (await alice.getIssue(issue.id)).description, { timeout: 20_000 })
    .toBe(expected);
  expect((await alice.getIssue(issue.id)).status).toBe("in_progress");
  await a.page.screenshot({ path: info.outputPath("issue-description-live.png"), animations: "disabled" });

  // After a refresh everyone reads the same Markdown.
  await b.page.reload();
  await expect.poll(() => descriptionText(b.page)).toBe(expected.replace("\n\n", "\n"));

  // One entry per live editor for the session, plus the API edit.
  const updates = (await alice.listIssueActivity(issue.id)).filter(
    (entry) => entry.action === "description_updated",
  );
  expect(updates.filter((entry) => entry.details?.live === "true")).toHaveLength(2);
  expect(updates).toHaveLength(3);
  await a.page.context().close();
  await b.page.context().close();
});

test("typing before the live session is ready keeps the solo editor until it loses focus", async ({ browser }) => {
  const issue = await alice.createIssue("Launch plan", { description: "Draft notes." });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await context.newPage();
  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
    localStorage.setItem("multica:chat:isOpen", "false");
  }, alice.getToken()!);
  let release = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  await page.route(`**/api/issues/${issue.id}/collab-ticket`, async (route) => {
    await held;
    await route.continue();
  });
  const ticket = page.waitForResponse((response) => response.url().endsWith(`/api/issues/${issue.id}/collab-ticket`));
  await page.goto(`/${slug}/issues/${issue.id}`);
  await expect(page.getByText("created this issue")).toBeVisible();

  await typeAt(page, "Draft notes.", "End", " First half.");
  await description(page).evaluate((element) => element.setAttribute("data-e2e-solo", ""));
  release();
  const live = ((await (await ticket).json()) as { enabled?: boolean }).enabled === true;
  test.skip(!live, "Live issue descriptions are off; start collab with FF_ISSUE_DESCRIPTION_LIVE=true.");
  // The live editor mounts hidden once it is ready.
  await expect(page.locator('[data-comment-content^="description:"] .ProseMirror')).toHaveCount(2, { timeout: 15_000 });

  await page.keyboard.type(" Second half.", { delay: 20 });
  await expect(page.locator("[data-e2e-solo]")).toBeFocused();
  await expect(page.locator("[data-e2e-solo]")).toHaveText("Draft notes. First half. Second half.");

  await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
  await expect(page.locator("[data-e2e-solo]")).toHaveCount(0);
  await expect.poll(() => descriptionText(page), { timeout: 20_000 }).toBe("Draft notes. First half. Second half.");
  await expect
    .poll(async () => (await alice.getIssue(issue.id)).description, { timeout: 20_000 })
    .toBe("Draft notes. First half. Second half.");
  await context.close();
});

test("the header shows who else is on the issue and where they are editing", async ({ browser }) => {
  const issue = await alice.createIssue("Launch plan", {
    description: "## Goals\n\nShip on Friday.\n\n## Risks\n\nThe import API may slip.",
  });
  const a = await openLive(browser, alice.getToken()!, issue.id);
  test.skip(!a.live, "Live issue descriptions are off; start collab with FF_ISSUE_DESCRIPTION_LIVE=true.");
  const chip = a.page.getByRole("button", { name: "1 viewing" });
  await expect(chip).toHaveCount(0);

  const b = await openLive(browser, bob.getToken()!, issue.id);
  await expect(chip).toBeVisible();
  await chip.click();
  await expect(a.page.getByText("Viewing · 1")).toBeVisible();
  await expect(a.page.locator('[data-presence-row="Bob Chen"]')).toBeVisible();
  await a.page.keyboard.press("Escape");

  await typeAt(b.page, "The import API may slip.", "End", " Bob owns it.");
  await chip.click();
  await expect(a.page.getByText("Editing · 1")).toBeVisible();
  await expect(a.page.getByText('In "Risks"')).toBeVisible();
  await a.page.locator('[data-presence-row="Bob Chen"]').click();
  await expect(a.page.locator('.collaboration-carets__caret[data-collaborator^="member:"]')).toBeInViewport();

  await b.page.context().close();
  await expect(chip).toHaveCount(0, { timeout: 15_000 });
  await a.page.context().close();
});
