import { test, expect, type Page } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

const API_BASE = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

/**
 * The four operating-system pages share one frame — OsPageShell (FIR-3589
 * items 5 and 7). Before it, these pages had no product page header and the
 * Roles chart simply ran off the bottom of the viewport with no way to reach
 * it. The existing operating-system spec covers Rocks and Strategy behaviour;
 * nothing covered the frame itself, on all four routes, which is exactly what
 * a deployed-build QA pass is asked to confirm (FIR-4191).
 */
/**
 * Each page names itself from its own terminology key, which is not always the
 * sidebar label: /strategy is reached by the "Strategy" link but titles itself
 * with strategy_map. Pin both so the header assertion stays honest about which
 * word each page is contracted to show.
 */
const TERMINOLOGY = {
  strategy: "Strategy",
  rock: "Rock",
  rocks: "Rocks",
  vision_plan: "Vision Plan",
  meetings: "Cycles",
  org_chart: "Roles",
  scorecard: "Scorecard",
  issues_list: "Issues List",
  strategy_map: "Strategy Map",
} as const;

const OS_PAGES = [
  { route: "rocks", navLabel: "Rocks", title: TERMINOLOGY.rocks },
  { route: "strategy", navLabel: "Strategy", title: TERMINOLOGY.strategy_map },
  { route: "meetings", navLabel: "Cycles", title: TERMINOLOGY.meetings },
  { route: "org-chart", navLabel: "Roles", title: TERMINOLOGY.org_chart },
] as const;

test.describe("Cerebro operating system page shell", () => {
  let api: TestApiClient;
  let slug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_operating_system", true);
    await request(api, "/api/cerebro/operating-system/settings", {
      method: "PUT",
      body: JSON.stringify({ terminology: TERMINOLOGY }),
    });
    for (const key of ["meetings", "org_chart"]) {
      await request(api, `/api/cerebro/operating-system/elements/${key}`, {
        method: "PUT",
        body: JSON.stringify({ enabled: true }),
      });
    }
    slug = await loginAsDefault(page);
  });

  test("gives every operating-system page a header carrying its own name", async ({ page }, testInfo) => {
    for (const { route, navLabel, title } of OS_PAGES) {
      await gotoOsPage(page, `/${slug}/${route}`);
      // The name must be the page's own top-line heading, not a sidebar link
      // that every operating-system page renders identically.
      const header = page.getByRole("heading", { name: title, exact: true, level: 1 });
      await expect(header, `${route} must show its own page header`).toBeVisible();
      await expect(header).toBeInViewport();
      // The header also carries the phone-width sidebar trigger, which is the
      // other half of item 5 — without it a phone cannot reach navigation.
      await expect(page.getByRole("link", { name: navLabel, exact: true })).toHaveCount(1);
      await page.screenshot({ path: testInfo.outputPath(`os-${route}.png`) });
    }
  });

  test("lets the Roles page reach content below the fold", async ({ page }) => {
    // An empty chart has nothing below the fold, so seed a tall one. This is
    // the shape that made item 7 visible in the first place: a real org chart
    // is taller than the viewport.
    const seatIds = await seedSeatChain(api, 12);
    try {
      // A short viewport forces the overflow that FIR-3589 item 7 fixed:
      // without OsPageShell's scrolling body, content below is unreachable.
      await page.setViewportSize({ width: 1280, height: 600 });
      await gotoOsPage(page, `/${slug}/org-chart`);
      const heading = page.getByRole("heading", { name: "Roles", exact: true, level: 1 });
      await expect(heading).toBeVisible();
      await expect(page.getByText("Seat 1", { exact: true }).first()).toBeVisible();

      const shell = await readShellBody(page);
      expect(shell.hasScrollContainer, "the Roles page body must own a scroll container").toBe(true);
      expect(shell.overflows, "the seeded chart must be taller than the viewport").toBe(true);

      const scrollTop = await scrollShellBodyBy(page, 400);
      expect(scrollTop, "scrolling the Roles body must move it").toBeGreaterThan(0);

      // The header is sticky, so reaching content below must never cost the page name.
      await expect(heading).toBeInViewport();
    } finally {
      for (const id of seatIds.reverse()) {
        await request(api, `/api/cerebro/org-chart/seats/${id}`, { method: "DELETE" });
      }
    }
  });

  test("lets the sidebar scroll from the top of the nav down to Settings", async ({ page }) => {
    // Same failure shape one level out: at a short viewport the sidebar's lower
    // entries sit below the fold, and the scrollbar is deliberately hidden.
    await page.setViewportSize({ width: 1280, height: 600 });
    await gotoOsPage(page, `/${slug}/rocks`);

    const settings = page.getByRole("link", { name: "Settings", exact: true });
    await expect(settings).toHaveCount(1);
    await settings.scrollIntoViewIfNeeded();
    await expect(settings).toBeInViewport();

    // Prove the scroll actually happened in the sidebar's own container rather
    // than the window, which is what makes the entry reachable by mouse wheel.
    const sidebarScrollTop = await page.evaluate(() => {
      const content = document.querySelector('[data-slot="sidebar-content"]');
      return content ? content.scrollTop : -1;
    });
    expect(sidebarScrollTop, "the sidebar must scroll in its own container").toBeGreaterThan(0);
  });
});

/**
 * The dev server compiles a route on its first visit, which can outlast the
 * default navigation wait. Wait for the document instead of every asset, then
 * let the per-assertion timeouts cover rendering.
 */
async function gotoOsPage(page: Page, path: string) {
  await page.goto(path, { waitUntil: "domcontentloaded", timeout: 60000 });
}

/**
 * OsPageShell renders a sticky header followed by the scrolling body, so the
 * body is the header's next sibling. Reading it that way asserts the page's
 * own frame rather than whichever div happens to overflow.
 */
const SHELL_BODY = `(() => {
  const heading = document.querySelector("h1");
  const header = heading ? heading.closest("div.sticky") : null;
  return header ? header.nextElementSibling : null;
})()`;

async function readShellBody(page: Page) {
  return page.evaluate(`(() => {
    const body = ${SHELL_BODY};
    if (!body) return { hasScrollContainer: false, overflows: false };
    return {
      hasScrollContainer: getComputedStyle(body).overflowY === "auto",
      overflows: body.scrollHeight > body.clientHeight + 1,
    };
  })()`) as Promise<{ hasScrollContainer: boolean; overflows: boolean }>;
}

async function scrollShellBodyBy(page: Page, delta: number) {
  return page.evaluate(`(() => {
    const body = ${SHELL_BODY};
    if (!body) return 0;
    body.scrollTop = ${delta};
    return body.scrollTop;
  })()`) as Promise<number>;
}

/** A chain of seats deep enough to push the chart past a short viewport. */
async function seedSeatChain(api: TestApiClient, count: number) {
  const ids: string[] = [];
  let parentId = "";
  for (let index = 0; index < count; index += 1) {
    const response = await request(api, "/api/cerebro/org-chart/seats", {
      method: "POST",
      body: JSON.stringify({
        name: `Seat ${index + 1}`,
        parent_id: parentId,
        responsibilities: ["Owns an outcome", "Reports the number", "Runs the cadence"],
        owners: [],
        position: index,
      }),
    });
    const seat = await response.json() as { id: string };
    ids.push(seat.id);
    parentId = seat.id;
  }
  return ids;
}

function request(api: TestApiClient, path: string, init: RequestInit = {}) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${api.getToken()}`,
      "X-Workspace-Slug": api.getWorkspaceSlug(),
      ...init.headers,
    },
  });
}
