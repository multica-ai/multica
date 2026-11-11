import { expect, test, type Locator, type Page } from "@playwright/test";
import { createTestApi, loginAsDefault, reloadAppPage } from "./helpers";
import type { TestApiClient, TestPullRequestSeed } from "./fixtures";

// Long names across repos, so every chip carries a repo prefix.
const PULL_REQUESTS: TestPullRequestSeed[] = [
  { repoName: "backend-service-platform", number: 4101, state: "open" },
  { repoName: "frontend-service-dashboard", number: 4102, state: "draft" },
  { repoName: "infrastructure-terraform-modules", number: 4103, state: "merged" },
  { repoName: "mobile-companion-application", number: 4104, state: "closed" },
];

/**
 * The PR chip group must fit its slot: shown chips and "+N" stay inside it,
 * no chip clips its own "#number", "+N" counts exactly the PRs not shown, and
 * the slot's own bounds hold (a card edge, a table cell).
 */
async function expectChipsFit(group: Locator, bounds: Locator) {
  await expect(group.getByTestId("issue-pull-request-chip").first()).toBeVisible();
  const boundsBox = await bounds.boundingBox();
  if (!boundsBox) throw new Error("Could not measure the chip group's slot");
  const layout = await group.evaluate((el) => {
    const chips = Array.from(
      el.querySelectorAll<HTMLElement>('[data-testid="issue-pull-request-chip"]'),
    );
    const overflow = el.querySelector<HTMLElement>(
      '[data-testid="issue-pull-request-overflow"]',
    );
    return {
      groupRight: el.getBoundingClientRect().right,
      chips: chips.map((chip) => ({
        right: chip.getBoundingClientRect().right,
        clipsItself: chip.scrollWidth > chip.clientWidth + 1,
        number:
          chip.querySelector('[data-testid="issue-pull-request-number"]')?.textContent ?? "",
      })),
      overflowText: overflow?.textContent ?? null,
      overflowRight: overflow?.getBoundingClientRect().right ?? null,
    };
  });

  // Whatever is rendered must be on screen: inside the group, and the group
  // inside its slot. Content past either edge is clipped or escapes.
  const edge = Math.min(layout.groupRight, boundsBox.x + boundsBox.width) + 0.5;
  expect(layout.groupRight).toBeLessThanOrEqual(boundsBox.x + boundsBox.width + 0.5);
  for (const chip of layout.chips) {
    expect(chip.right).toBeLessThanOrEqual(edge);
    expect(chip.clipsItself).toBe(false);
  }
  if (layout.overflowRight !== null) {
    expect(layout.overflowRight).toBeLessThanOrEqual(edge);
  }
  // A squeezed chip gives up its repo name, never its number.
  for (const chip of layout.chips) {
    expect(chip.number).toMatch(/^#410\d$/);
  }
  const hidden = PULL_REQUESTS.length - layout.chips.length;
  expect(layout.overflowText).toBe(hidden > 0 ? `+${hidden}` : null);
}

async function openDisplay(page: Page) {
  await page.getByRole("button", { name: "Display", exact: true }).click();
}

async function switchView(page: Page, to: string) {
  await openDisplay(page);
  const layout = page.getByRole("group", { name: "View", exact: true }).getByRole("button", {
    name: to,
    exact: true,
  });
  await layout.click();
  await expect(layout).toHaveAttribute("aria-pressed", "true");
  await page.keyboard.press("Escape");
}

test.describe("Issue pull request chips", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await page.setViewportSize({ width: 1024, height: 800 });
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api?.cleanup();
  });

  test("fit long repo names into board cards, list rows and table cells", async ({
    page,
  }) => {
    const title = `PR chips ${Date.now().toString(36)}`;
    const issue = await api.createIssue(title, { status: "todo" });
    await api.linkPullRequests(issue.id, PULL_REQUESTS);
    await reloadAppPage(page);

    await openDisplay(page);
    await page.getByRole("button", { name: "Pull requests", exact: true }).click();
    await page.keyboard.press("Escape");

    const card = page.locator("[data-board-card]").filter({ hasText: title });
    await expectChipsFit(card.getByTestId("issue-pull-request-chips"), card);

    await switchView(page, "List");
    const row = page.locator("[data-slot=issue-list-row]").filter({ hasText: title });
    await expectChipsFit(row.getByTestId("issue-pull-request-chips"), row);
    // The chips yield to the title, not the other way round.
    const titleBox = await row.getByText(title, { exact: true }).boundingBox();
    expect(titleBox?.width ?? 0).toBeGreaterThan(80);

    await switchView(page, "Table");
    await openDisplay(page);
    await page.getByRole("button", { name: /^\d+ shown$/ }).click();
    await page.getByRole("menuitemcheckbox", { name: "Pull requests", exact: true }).click();
    await page.keyboard.press("Escape");
    await page.keyboard.press("Escape");
    const cellChips = page
      .getByRole("row")
      .filter({ hasText: title })
      .getByTestId("issue-pull-request-chips");
    await expectChipsFit(cellChips, cellChips.locator("xpath=ancestor::td[1]"));
  });
});
