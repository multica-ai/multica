import { readFileSync } from "node:fs";
import { expect, test } from "@playwright/test";
import pg from "pg";
import { SUPPORTED_LOCALES } from "../packages/core/i18n/types";
import { TestApiClient } from "./fixtures";

// MUL-9930: the Work panel's Inbox section header has been reworked three
// times in a week (#10614, #11259, #11268), and #11259 shipped with every
// tab label cut down to its first letter at the panel's default width. At
// that width, in every language and with any tab open, no tab label may
// truncate and no tab may run under the header's actions (filter, ⋯).
// Narrower widths are not covered: there Spanish and French still truncate
// the open tab.
const DEFAULT_SIDEBAR_WIDTH = 280;
const TABS = ["inbox", "snoozed", "archived"] as const;
// The counts take room beside the labels; an empty inbox would hide them.
const UNREAD = 12;
const SNOOZED = 2;

const inboxTabLabel = Object.fromEntries(
  SUPPORTED_LOCALES.map((locale) => {
    const inbox = JSON.parse(
      readFileSync(new URL(`../packages/views/locales/${locale}/inbox.json`, import.meta.url), "utf8"),
    ) as { page: { title: string } };
    return [locale, inbox.page.title];
  }),
);

test("Work panel Inbox header keeps every tab label whole and clear of its actions at the default width, in every language", async ({
  page,
  context,
  baseURL,
}) => {
  if (!baseURL) throw new Error("The Inbox header regression requires a baseURL");
  // An account of its own: no stored language overrides the locale cookie,
  // and the counts are exactly the ones seeded here.
  const api = new TestApiClient();
  const run = `${Date.now()}-${process.pid}`;
  const login = await api.login(`inbox-header-${run}@localhost`, "Inbox header test");
  const workspace = await api.ensureWorkspace("Inbox header", `inbox-header-${run}`);
  await api.markUserOnboarded();
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  try {
    await db.query(
      `INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, title)
       SELECT $1, 'member', $2, 'system', 'Unread ' || n FROM generate_series(1, $3::int) n`,
      [workspace.id, login.user.id, UNREAD],
    );
    for (let n = 1; n <= SNOOZED; n++) {
      const issue = await api.createIssue(`Snoozed ${n}`);
      await db.query(
        `INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, issue_id, type, title, snoozed_at, snoozed_until)
         VALUES ($1, 'member', $2, $3, 'system', $4, now(), now() + interval '1 day')`,
        [workspace.id, login.user.id, issue.id, `Snoozed ${n}`],
      );
    }

    // loginAsDefault waits for the English "New Issue" label, which never
    // appears once the language changes.
    await page.addInitScript(
      ({ token, width }) => {
        localStorage.setItem("multica_token", token);
        localStorage.setItem("multica:chat:isOpen", "false");
        localStorage.setItem("sidebar_width", String(width));
      },
      { token: api.getToken()!, width: DEFAULT_SIDEBAR_WIDTH },
    );
    await page.setViewportSize({ width: 1440, height: 900 });

    const tab = (id: string) => page.locator(`[data-inbox-tab="${id}"]`);
    const problems: string[] = [];
    for (const [index, locale] of SUPPORTED_LOCALES.entries()) {
      await context.addCookies([{ name: "multica-locale", value: locale, url: baseURL }]);
      if (index === 0) await page.goto(`/${workspace.slug}/issues`, { waitUntil: "domcontentloaded" });
      else await page.reload({ waitUntil: "domcontentloaded" });
      await expect(tab("inbox"), `${locale}: Inbox tab label`).toContainText(inboxTabLabel[locale]!, {
        timeout: 30000,
      });
      await expect(tab("inbox"), `${locale}: unread count`).toContainText(String(UNREAD));
      await page.evaluate(() => document.fonts.ready);

      for (const id of TABS) {
        // Selecting the open tab folds the section, so only select a closed one.
        if ((await tab(id).getAttribute("aria-selected")) !== "true") await tab(id).click();
        await expect(tab(id)).toHaveAttribute("aria-selected", "true");
        if (id === "snoozed") await expect(tab(id), `${locale}: snoozed count`).toContainText(String(SNOOZED));

        const found = await tab(id).evaluate(async (openTab) => {
          const tablist = openTab.closest<HTMLElement>('[role="tablist"]')!;
          const header = tablist.closest("section")!.firstElementChild as HTMLElement;
          // A switch slides the highlight and unfolds the new tab's name:
          // measure once the tabs have held still for a few frames.
          const frame = () => new Promise(requestAnimationFrame);
          const layout = () =>
            [...tablist.querySelectorAll("*")].map((el) => el.getBoundingClientRect().width).join();
          for (let still = 0, last = "", n = 0; still < 5 && n < 120; n++) {
            await frame();
            const now = layout();
            still = now === last ? still + 1 : 0;
            last = now;
          }
          // Visually hidden copy (sr-only names, the open section's chevron)
          // is 1px wide by design and takes no part in the layout, and so is
          // a closed tab's name, folded to nothing beside its icon.
          const shown = (el: Element) => {
            for (let node: Element | null = el; node && node !== tablist; node = node.parentElement) {
              if (node.getBoundingClientRect().width <= 1) return false;
            }
            return true;
          };
          const name = (el: Element) =>
            el.getAttribute("aria-label") || el.getAttribute("data-inbox-tab") || el.textContent?.trim();
          const tabs = [...tablist.querySelectorAll<HTMLElement>("[data-inbox-tab]")];
          const actions = [...header.querySelectorAll("button")].filter(
            (button) => !tablist.parentElement!.contains(button) && shown(button),
          );
          const problems = actions.length === 0 ? ["no actions found in the header"] : [];

          for (const tabEl of tabs) {
            for (const el of [tabEl, ...tabEl.querySelectorAll<HTMLElement>("*")]) {
              if (!shown(el) || !el.textContent?.trim()) continue;
              if (el.scrollWidth > el.clientWidth) {
                problems.push(
                  `${name(tabEl)} tab "${el.textContent.trim()}" is truncated (scrollWidth ${el.scrollWidth} > clientWidth ${el.clientWidth})`,
                );
              }
            }
            const box = tabEl.getBoundingClientRect();
            for (const action of actions) {
              const other = action.getBoundingClientRect();
              if (box.right > other.left && box.left < other.right) {
                problems.push(
                  `${name(tabEl)} tab (right edge ${Math.round(box.right)}) overlaps "${name(action)}" (left edge ${Math.round(other.left)})`,
                );
              }
            }
          }
          return problems;
        });
        problems.push(...found.map((problem) => `${locale} · ${id} open: ${problem}`));
      }
    }

    expect(problems).toEqual([]);
  } finally {
    await db.end();
    await api.deleteWorkspace();
  }
});
