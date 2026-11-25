import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { API_BASE, TestApiClient } from "./fixtures";

// Guests are deny-by-default: the API answers a guest only on the routes
// allowlisted in server/internal/middleware/guest.go, while guest pages reuse
// member components. A member component that starts reading a route the list
// lacks fails with 403 for a guest only, which no member-side test notices
// (MUL-9873). This walks every page a guest can reach and fails on any 4xx
// response or page error, and on member write actions showing up.

const RUN = `${process.env.TEST_WORKER_INDEX ?? "0"}-${process.env.E2E_RUN_ID ?? `${Date.now().toString(36)}-${process.pid.toString(36)}`}`;

type ProjectKey = "shared" | "private" | "archived" | "internal";
type IssueKey = "review" | "form" | "export" | "contract" | "migration" | "pricing";

interface GuestFixture {
  guest_token: string;
  projects: Record<ProjectKey, { id: string; title: string }>;
  issues: Record<IssueKey, { id: string; identifier: string; title: string }>;
}

let fixture: GuestFixture;
let slug: string;
let owner: TestApiClient;

// `scripts/dev-fixtures.mjs guest` builds the scenario, run against a
// workspace of its own so the guest and its projects stay out of other specs.
// The fixture reads an environment profile; this one holds the owner's token
// and the guest's, signed in here so no fixed verification code is needed.
test.beforeAll(async ({}, testInfo) => {
  owner = new TestApiClient();
  await owner.login(`e2e-guest-owner-${RUN}@multica.ai`, "E2E Guest Owner");
  const workspace = await owner.ensureWorkspace(`E2E Guest ${RUN}`, `e2e-guest-${RUN}`);
  await owner.markUserOnboarded();
  slug = workspace.slug;

  const guestEmail = `e2e-guest-${RUN}@multica.ai`;
  const guest = new TestApiClient();
  await guest.login(guestEmail, "E2E Guest");

  const profiles = mkdtempSync(join(tmpdir(), "multica-e2e-guest-"));
  try {
    const profile = join(profiles, "e2e-guest");
    mkdirSync(profile);
    writeFileSync(
      join(profile, "config.json"),
      JSON.stringify({
        token: owner.getToken(),
        workspace_id: workspace.id,
        server_url: API_BASE,
        app_url: testInfo.project.use.baseURL,
      }),
    );
    writeFileSync(join(profile, "guest.json"), JSON.stringify({ email: guestEmail, token: guest.getToken() }));
    // Without PORT and FRONTEND_PORT the fixture takes both URLs from the profile.
    const { PORT: _port, FRONTEND_PORT: _frontendPort, ...env } = process.env;
    const output = execFileSync(
      process.execPath,
      [resolve("scripts/dev-fixtures.mjs"), "guest", "--json", "--guest-email", guestEmail],
      { env: { ...env, MULTICA_DEV_PROFILES_HOME: profiles, MULTICA_DEV_PROFILE: "e2e-guest" }, encoding: "utf8" },
    );
    fixture = JSON.parse(output).guest;
  } finally {
    rmSync(profiles, { recursive: true, force: true });
  }
});

test.afterAll(async () => {
  await owner?.deleteWorkspace();
});

interface PageWatch {
  problems: string[];
  settled: () => Promise<void>;
}

/**
 * Records every 4xx/5xx response and page error from here on. `notFound`
 * lists URL fragments whose 404 is the expected answer.
 */
function watch(page: Page, { notFound = [] as string[] } = {}): PageWatch {
  const problems: string[] = [];
  let inFlight = 0;
  let lastActivity = Date.now();
  const isApi = (url: string) => url.startsWith(API_BASE) || new URL(url).pathname.startsWith("/api/");
  page.on("request", (request) => {
    if (!isApi(request.url())) return;
    inFlight++;
    lastActivity = Date.now();
  });
  const finished = (request: { url(): string }) => {
    if (!isApi(request.url())) return;
    inFlight--;
    lastActivity = Date.now();
  };
  page.on("requestfinished", finished);
  page.on("requestfailed", finished);
  page.on("response", (response) => {
    if (response.status() < 400) return;
    // The server shedding load ("busy, retry shortly"); the client retries.
    if (response.status() === 503 && response.headers()["retry-after"]) return;
    const url = response.url();
    if (response.status() === 404 && notFound.some((fragment) => url.includes(fragment))) return;
    problems.push(`${response.status()} ${response.request().method()} ${url}`);
  });
  page.on("pageerror", (error) => problems.push(`page error: ${error.message}`));

  // Done once the page has made no API request for a second: a component
  // that mounts late still gets its requests answered before the check.
  const settled = () =>
    expect
      .poll(() => inFlight <= 0 && Date.now() - lastActivity > 1000, { timeout: 20_000, intervals: [100] })
      .toBe(true);
  return { problems, settled };
}

async function openAsGuest(page: Page, path: string, options?: { notFound?: string[] }) {
  await page.addInitScript((token) => {
    localStorage.setItem("multica_token", token);
    localStorage.setItem("multica:chat:isOpen", "false");
  }, fixture.guest_token);
  const seen = watch(page, options);
  await page.goto(path, { waitUntil: "domcontentloaded" });
  return seen;
}

async function expectClean(seen: PageWatch) {
  await seen.settled();
  expect(seen.problems, "requests the guest was refused, and page errors").toEqual([]);
}

const PROJECT_ACTIONS = /(rename|archive|restore|delete) project/i;

/**
 * Write actions only a member has: creating issues and managing projects,
 * including the archived-project banner's "Restore". Run after the page has
 * settled, since `toHaveCount(0)` passes before late content mounts.
 */
async function expectNoMemberActions(page: Page) {
  await expect(page.getByRole("button", { name: /new issue|add issue|create issue/i })).toHaveCount(0);
  await expect(page.getByRole("button", { name: /^restore$/i })).toHaveCount(0);
  await expect(page.getByRole("button", { name: PROJECT_ACTIONS })).toHaveCount(0);
}

/** The project title menu holds the project actions; they exist only while it is open. */
async function expectGuestProjectMenu(page: Page, seen: PageWatch, title: string) {
  await page.getByRole("heading", { level: 1 }).getByRole("button", { name: title }).click();
  const menu = page.getByRole("menu");
  await expect(menu.getByRole("menuitem", { name: "Copy link", exact: true })).toBeVisible();
  await expect(menu.getByRole("menuitem", { name: PROJECT_ACTIONS })).toHaveCount(0);
  await page.keyboard.press("Escape");
  await expect(menu).toHaveCount(0);
  await expectClean(seen);
}

/** A guest's whole issue menu: the server lets it change nothing about the issue. */
const GUEST_ISSUE_MENU = [
  "Open in new tab",
  "Open in Desktop",
  "Pin to sidebar",
  "Copy link",
  "Copy link with title",
  "Copy issue ID",
];

/** The palette's issue commands a guest keeps, mirroring the server's guest allowlist. */
const GUEST_ISSUE_COMMANDS = [
  "issue-copy-link",
  "issue-copy-identifier",
  "issue-pin",
  "issue-subscribe",
  "issue-fold-comments",
  "issue-unfold-comments",
];

function issueRow(page: Page, title: string) {
  return page.locator('[data-mc="issue-row"]').filter({ hasText: title });
}

/** Board cards cannot be selected; list rows can. */
async function showAsList(page: Page) {
  await page.getByRole("button", { name: "Display", exact: true }).click();
  const popover = page.locator("[data-slot=popover-content]");
  const list = popover.getByRole("button", { name: "List", exact: true });
  await list.click();
  await expect(list).toHaveAttribute("aria-pressed", "true");
  await page.keyboard.press("Escape");
  await expect(popover).toBeHidden();
}

test.describe("as a guest", () => {
  test("Inbox and the Work panel show only what is shared", async ({ page }) => {
    const seen = await openAsGuest(page, `/${slug}/inbox`);
    const shared = page.getByRole("region", { name: "Shared with you" });
    await expect(shared).toBeVisible({ timeout: 30_000 });
    await expect(shared.getByText(fixture.projects.shared.title, { exact: true })).toBeVisible();
    await expect(shared.getByText(fixture.projects.private.title, { exact: true })).toBeVisible();
    await expect(page.getByText(fixture.projects.internal.title)).toHaveCount(0);
    const inbox = page.getByRole("region", { name: "Inbox" });
    for (const key of ["review", "form", "export"] as const) {
      await expect(inbox.getByText(fixture.issues[key].title, { exact: true })).toBeVisible();
    }
    await expect(page.getByText(fixture.issues.pricing.title)).toHaveCount(0);
    await expectClean(seen);
    await expectNoMemberActions(page);

    await inbox.getByText(fixture.issues.review.title, { exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`/${slug}/inbox\\?issue=${fixture.issues.review.id}`));
    await page.getByRole("button", { name: "Toggle right sidebar" }).click();
    await expect(page.getByText("Guests can view and comment, but cannot change issue properties.")).toBeVisible();
    await expectClean(seen);
    await expectNoMemberActions(page);

    await page.getByRole("link", { name: "Involved", exact: true }).click();
    await expect(page.getByText(fixture.issues.export.title).first()).toBeVisible();
    await expectClean(seen);
    await expectNoMemberActions(page);
  });

  for (const [key, kind] of [["shared", "workspace-visible"], ["private", "private"], ["archived", "archived"]] as const) {
    test(`a shared ${kind} project opens on every tab`, async ({ page }) => {
      const project = fixture.projects[key];
      const seen = await openAsGuest(page, `/${slug}/projects/${project.id}?tab=overview`);
      await expect(page.getByText(project.title).first()).toBeVisible({ timeout: 30_000 });
      await expect(page.getByText("Guests can view this project, but cannot change its properties.")).toBeVisible();
      if (key === "archived") await expect(page.getByText("This project is archived.")).toBeVisible();
      await expectClean(seen);
      await expectNoMemberActions(page);
      await expectGuestProjectMenu(page, seen, project.title);

      for (const tab of ["Issues", "Activity", "Overview"]) {
        await page.getByRole("tab", { name: new RegExp(`^${tab}`) }).click();
        await expect(page).toHaveURL(new RegExp(`tab=${tab.toLowerCase()}`));
        await expectClean(seen);
        await expectNoMemberActions(page);
        await expectGuestProjectMenu(page, seen, project.title);
      }
    });
  }

  for (const [key, kind] of [["review", "workspace-visible"], ["contract", "private"], ["migration", "archived"]] as const) {
    test(`an issue in the shared ${kind} project opens`, async ({ page }) => {
      const issue = fixture.issues[key];
      const seen = await openAsGuest(page, `/${slug}/issues/${issue.identifier}`);
      await expect(page.getByText(issue.title).first()).toBeVisible({ timeout: 30_000 });
      await expect(page.getByText("Guests can view and comment, but cannot change issue properties.")).toBeVisible();
      await expectClean(seen);
      await expectNoMemberActions(page);
    });
  }

  test("an issue's right-click menu only opens, pins and copies", async ({ page }) => {
    const { shared } = fixture.projects;
    const seen = await openAsGuest(page, `/${slug}/projects/${shared.id}?tab=issues`);
    const card = page.locator("[data-board-card]").filter({ hasText: fixture.issues.review.title });
    await expect(card).toBeVisible({ timeout: 30_000 });
    await expectClean(seen);

    await card.click({ button: "right" });
    const menu = page.getByRole("menu");
    // Opening the menu used to load the issue's runs and the quick actions,
    // both refused to a guest (MUL-9883).
    await expect(menu.getByRole("menuitem")).toHaveText(GUEST_ISSUE_MENU);
    await expectClean(seen);
    await page.keyboard.press("Escape");
    await expect(menu).toHaveCount(0);
  });

  test("a selection only copies links", async ({ page }) => {
    const { shared } = fixture.projects;
    const seen = await openAsGuest(page, `/${slug}/projects/${shared.id}?tab=issues`);
    await expect(page.getByText(fixture.issues.review.title).first()).toBeVisible({ timeout: 30_000 });
    await showAsList(page);

    for (const key of ["review", "export"] as const) {
      const row = issueRow(page, fixture.issues[key].title);
      await row.hover();
      await row.getByRole("checkbox").check();
    }
    const bar = page.getByRole("toolbar", { name: "2 selected" });
    await expect(bar).toBeVisible();
    await expect(bar.getByRole("button")).toHaveCount(2);
    await expect(bar.getByRole("button", { name: "Clear selection" })).toBeVisible();
    await expect(bar.getByRole("button", { name: "Copy links" })).toBeVisible();
    await expectClean(seen);

    await issueRow(page, fixture.issues.review.title).click({ button: "right" });
    const menu = page.getByRole("menu");
    await expect(menu.getByRole("menuitem")).toHaveText(["Copy links", "Clear selection"]);
    await expectClean(seen);
    await page.keyboard.press("Escape");
    await expect(menu).toHaveCount(0);
  });

  test("the command palette offers an issue's guest commands and searches cleanly", async ({ page }) => {
    const issue = fixture.issues.review;
    const seen = await openAsGuest(page, `/${slug}/issues/${issue.identifier}`);
    await expect(page.getByText(issue.title).first()).toBeVisible({ timeout: 30_000 });
    await expectClean(seen);

    await page.keyboard.press("ControlOrMeta+k");
    const input = page.getByPlaceholder("Search, or type what needs doing...");
    await expect(input).toBeVisible();
    await input.fill(">");
    const issueCommands = page.locator('[cmdk-item][data-value^="command:context-issue:"]');
    await expect
      .poll(() => issueCommands.evaluateAll((rows) => rows.map((row) => row.getAttribute("data-value")).sort()))
      .toEqual(GUEST_ISSUE_COMMANDS.map((id) => `command:context-issue:${id}`).sort());
    // The global commands are listed too, so their absence below is not vacuous.
    await expect(page.getByRole("option", { name: "Change theme..." })).toBeVisible();
    for (const name of ["New issue", "New project", "New autopilot", "Chat with an agent"]) {
      await expect(page.getByRole("option", { name, exact: true })).toHaveCount(0);
    }
    await expectClean(seen);

    // Typing loads the autopilot, skill and doc lists and searches docs on the
    // server, all refused to a guest.
    await input.fill("sign-up");
    await expect(page.getByRole("option", { name: new RegExp(issue.title) }).first()).toBeVisible();
    // The doc search fires DOC_SEARCH_DEBOUNCE_MS (300 ms) after the last keystroke.
    await page.waitForTimeout(500);
    await expectClean(seen);
  });

  test("a project that is not shared is not found", async ({ page }) => {
    const { id } = fixture.projects.internal;
    const seen = await openAsGuest(page, `/${slug}/projects/${id}`, { notFound: [id] });
    await expect(page.getByText("Project not found")).toBeVisible({ timeout: 30_000 });
    await expectClean(seen);
  });

  test("an issue that is not shared is not found", async ({ page }) => {
    const { id, identifier } = fixture.issues.pricing;
    const seen = await openAsGuest(page, `/${slug}/issues/${identifier}`, { notFound: [id, identifier] });
    await expect(page.getByText("Issue not found")).toBeVisible({ timeout: 30_000 });
    await expectClean(seen);
  });
});
