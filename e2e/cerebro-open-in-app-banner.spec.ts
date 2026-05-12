import { test, expect, devices, type Page } from "@playwright/test";
import { TestApiClient } from "./fixtures";

const IPHONE = devices["iPhone 14"];
const E2E_EMAIL = "e2e@multica.ai";
const E2E_NAME = "E2E User";
const E2E_WORKSPACE_SLUG = "e2e-workspace";

/**
 * Mobile-aware login. The shared `loginAsDefault` asserts that the desktop
 * sidebar's "Inbox" link is visible, which fails on phone viewports where the
 * sidebar collapses behind a menu. For this banner test we only need an authed
 * page on a workspace URL — not the desktop chrome — so we do the API login +
 * token injection inline and skip the desktop assertion.
 */
async function authedGoto(page: Page, path: string) {
  const api = new TestApiClient();
  await api.login(E2E_EMAIL, E2E_NAME);
  const ws = await api.ensureWorkspace("E2E Workspace", E2E_WORKSPACE_SLUG);
  await api.dismissStarterContent();
  const token = api.getToken();
  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
  }, token);
  await page.goto(`/${ws.slug}${path}`, { waitUntil: "domcontentloaded" });
  return ws.slug;
}

test.describe("Open-in-app banner (mobile PWA fallback)", () => {
  test.use({
    viewport: IPHONE.viewport,
    userAgent: IPHONE.userAgent,
    deviceScaleFactor: IPHONE.deviceScaleFactor,
    isMobile: IPHONE.isMobile,
    hasTouch: IPHONE.hasTouch,
  });

  test("renders inside a workspace on iPhone Safari with a same-scope link", async ({ page }) => {
    const slug = await authedGoto(page, "/issues");
    const banner = page.getByTestId("open-in-app-banner");
    await expect(banner).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole("region", { name: "Open in app" })).toBeVisible();
    const open = page.getByTestId("open-in-app-banner-open");
    // Default link target is the current pathname — must remain in scope so
    // Android Chrome's handle_links can capture it.
    await expect(open).toHaveAttribute("href", new RegExp(`/${slug}/issues`));
  });

  test("dismiss hides the banner and persists across navigation", async ({ page }) => {
    test.setTimeout(90000);
    const slug = await authedGoto(page, "/issues");
    const banner = page.getByTestId("open-in-app-banner");
    await expect(banner).toBeVisible({ timeout: 10000 });
    await page.getByTestId("open-in-app-banner-dismiss").click();
    await expect(banner).toHaveCount(0);

    // First hit on /agents in dev mode triggers a slow Turbopack compile.
    await page.goto(`/${slug}/agents`, { waitUntil: "domcontentloaded", timeout: 60000 });
    // Banner stays dismissed because we share one localStorage key per origin.
    await expect(page.getByTestId("open-in-app-banner")).toHaveCount(0);
  });

  test("does not render when the page reports it is running as a standalone PWA", async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, "standalone", {
        value: true,
        configurable: true,
      });
      const original = window.matchMedia.bind(window);
      window.matchMedia = (query: string) => {
        if (query.includes("standalone")) {
          return {
            matches: true,
            media: query,
            onchange: null,
            addEventListener: () => {},
            removeEventListener: () => {},
            addListener: () => {},
            removeListener: () => {},
            dispatchEvent: () => false,
          } as MediaQueryList;
        }
        return original(query);
      };
    });
    await authedGoto(page, "/issues");
    await expect(page.getByTestId("open-in-app-banner")).toHaveCount(0);
  });
});

test.describe("Open-in-app banner (desktop)", () => {
  test("does not render on desktop Chrome", async ({ page }) => {
    await authedGoto(page, "/issues");
    await expect(page.getByTestId("open-in-app-banner")).toHaveCount(0);
  });
});
