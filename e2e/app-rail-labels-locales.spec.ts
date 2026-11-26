import { readFileSync } from "node:fs";
import { expect, test } from "@playwright/test";
import { SUPPORTED_LOCALES } from "../packages/core/i18n/types";
import { RAIL_SPACES as SPACES, type RailSpaceId } from "../packages/core/paths/spaces";
import { TestApiClient } from "./fixtures";

// MUL-9942: a rail space tile is 48px wide and its label gets 44px at the
// default font size. "Autopilot", "オートパイロット", "Automatización" and
// friends were cut to "Autop…" there, so each language's `rail.*` label is a
// short name that must fit whole; the full name stays in the tooltip. A
// selected tile sets its label in a heavier weight, so every tile is checked
// while it is selected too. Larger Appearance font sizes are not covered: the
// label grows with them and the tile does not.
//
// The tiles come from RAIL_SPACES, so a new rail space is checked as soon as
// it is added. A space behind a feature flag renders no tile until the flag is
// on, and this test then fails waiting for it: turn the flag on in CI's e2e
// job rather than skipping the space here. Docs needs `FF_DOCS_V1=true` this
// way.

const railLabels = Object.fromEntries(
  SUPPORTED_LOCALES.map((locale) => {
    const layout = JSON.parse(
      readFileSync(new URL(`../packages/views/locales/${locale}/layout.json`, import.meta.url), "utf8"),
    ) as { rail: Record<RailSpaceId, string> };
    return [locale, layout.rail];
  }),
);

test("app rail shows every space label whole, selected or not, in every language", async ({
  page,
  context,
  baseURL,
}) => {
  test.setTimeout(240000);
  if (!baseURL) throw new Error("The app rail label regression requires a baseURL");
  const labelSets = SUPPORTED_LOCALES.map((locale) => SPACES.map((space) => railLabels[locale]![space]).join(" / "));
  expect(new Set(labelSets).size, "every language needs its own set of rail labels").toBe(SUPPORTED_LOCALES.length);
  // An account of its own, so no stored language overrides the locale cookie.
  const api = new TestApiClient();
  const run = `${Date.now()}-${process.pid}`;
  await api.login(`rail-labels-${run}@localhost`, "Rail labels test");
  const workspace = await api.ensureWorkspace("Rail labels", `rail-labels-${run}`);
  await api.markUserOnboarded();
  try {
    // loginAsDefault waits for the English "New Issue" label, which never
    // appears once the language changes.
    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken()!);
    await page.setViewportSize({ width: 1440, height: 900 });

    const tile = (space: string) => page.locator(`[data-slot="app-rail"] a[data-space="${space}"]`);
    const problems: string[] = [];
    for (const [index, locale] of SUPPORTED_LOCALES.entries()) {
      await context.addCookies([{ name: "multica-locale", value: locale, url: baseURL }]);
      if (index === 0) await page.goto(`/${workspace.slug}/issues`, { waitUntil: "domcontentloaded" });
      else await page.reload({ waitUntil: "domcontentloaded" });
      // Several languages share a label ("Auto" in en, fr and es), so only
      // the whole set of labels proves the page has switched to this one.
      for (const space of SPACES) {
        await expect(tile(space), `${locale}: ${space} label`).toHaveText(railLabels[locale]![space], {
          timeout: 30000,
        });
      }
      await page.evaluate(() => document.fonts.ready);

      for (const selected of SPACES) {
        await tile(selected).click();
        await expect(tile(selected)).toHaveAttribute("aria-current", "page");
        const found = await page.evaluate((spaces) => {
          const clipped: string[] = [];
          for (const space of spaces) {
            const link = document.querySelector(`[data-slot="app-rail"] a[data-space="${space}"]`)!;
            for (const el of [link, ...link.querySelectorAll<HTMLElement>("*")]) {
              if (!(el instanceof HTMLElement) || !el.textContent?.trim()) continue;
              if (el.scrollWidth > el.clientWidth) {
                clipped.push(
                  `${space} "${el.textContent.trim()}" is truncated (scrollWidth ${el.scrollWidth} > clientWidth ${el.clientWidth})`,
                );
              }
            }
          }
          return clipped;
        }, SPACES);
        problems.push(...found.map((problem) => `${locale} · ${selected} selected: ${problem}`));
      }
    }

    expect(problems).toEqual([]);
  } finally {
    await api.deleteWorkspace();
  }
});
