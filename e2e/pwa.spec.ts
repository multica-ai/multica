import { test, expect } from "@playwright/test";
import { loginAsDefault, waitForPageText } from "./helpers";

interface ManifestIcon {
  src: string;
  sizes: string;
  type: string;
  purpose: string;
}

interface Manifest {
  id: string;
  start_url: string;
  icons: ManifestIcon[];
}

test.describe("PWA", () => {
  test("manifest is served and declares the install surface", async ({ request }) => {
    const res = await request.get("/manifest.webmanifest");
    expect(res.status()).toBe(200);

    const manifest = (await res.json()) as Manifest;
    expect(manifest.id).toBe("/");
    expect(manifest.start_url).toBe("/?source=pwa");

    const sizes = manifest.icons.map((icon) => icon.sizes);
    expect(sizes).toContain("192x192");
    expect(sizes).toContain("512x512");

    for (const icon of manifest.icons) {
      const iconRes = await request.get(icon.src);
      expect(iconRes.status(), `icon ${icon.src} should resolve`).toBe(200);
    }
  });

  test("service worker takes control on a workspace page", async ({ page }) => {
    await loginAsDefault(page);

    // Registration happens in the workspace layout; sw.js calls skipWaiting
    // and clients.claim, so the page becomes controlled without a reload.
    await page.waitForFunction(() => navigator.serviceWorker.controller !== null, undefined, {
      timeout: 30000,
    });

    const scriptURL = await page.evaluate(
      () => navigator.serviceWorker.controller?.scriptURL ?? null,
    );
    expect(scriptURL).toContain("/sw.js");
  });

  test("status bar colour tracks the app theme, not the OS", async ({ page }) => {
    const workspaceSlug = await loginAsDefault(page);

    // What the status bar should be: the colour the page actually paints.
    const paintedBackground = () =>
      page.evaluate(() => getComputedStyle(document.body).backgroundColor);

    // Media-scoped metas follow the OS and would shadow the dynamic one, so
    // exactly one unconditional meta should survive hydration.
    const themeColor = async () => {
      const metas = page.locator("meta[name='theme-color']");
      await expect(metas).toHaveCount(1);
      await expect(metas).not.toHaveAttribute("media", /.*/);
      return metas.getAttribute("content");
    };

    const light = await themeColor();
    expect(light).toBe(await paintedBackground());
    expect(light).toMatch(/^rgba?\(/);

    await page.goto(`/${workspaceSlug}/settings?tab=preferences`, {
      waitUntil: "domcontentloaded",
    });
    await waitForPageText(page, "Theme");

    // Force the app dark while the OS stays light.
    await page.getByRole("combobox", { name: "Theme" }).click();
    await page.getByRole("option", { name: "Dark", exact: true }).click();
    await expect(page.locator("html")).toHaveClass(/dark/);

    // Retinted in place: no reload happened between the two reads.
    const dark = await paintedBackground();
    const meta = page.locator("meta[name='theme-color']");
    await expect(meta).toHaveAttribute("content", dark);
    await expect(meta).toHaveAttribute("content", /^rgba?\(/);
  });

  test("no service worker registers on the marketing page", async ({ page }) => {
    await page.goto("/", { waitUntil: "networkidle" });

    const registrations = await page.evaluate(() =>
      navigator.serviceWorker.getRegistrations().then((regs) => regs.length),
    );
    expect(registrations).toBe(0);
  });
});
