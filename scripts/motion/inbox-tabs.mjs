// Example scenario for scripts/motion/capture.mjs: switching the Work panel's
// Inbox tabs (Inbox / Snoozed / Archived). It needs the `sidebar` fixture:
//
//   make env-exec ARGS="-- node scripts/dev-fixtures.mjs sidebar"
//   make env-exec ARGS="-- node scripts/motion/capture.mjs scripts/motion/inbox-tabs.mjs --out ../motion"
//
// It snoozes two of the fixture's notifications and archives three, so every
// tab has rows. Archived loads on its first open; that request is held for
// 600ms to show what the section does while a tab loads.
//
// Scenario options: --locale zh-CN (default en-US).

const SECTION = 'section[data-mc="panel-section"]:has([data-inbox-tab])';
const TARGETS = {
  // The sliding highlight when there is one, else the open tab's own background.
  indicator: `${SECTION} [role="tablist"] > span[aria-hidden], ${SECTION} [role="tab"][aria-selected="true"]`,
  tab: `${SECTION} [role="tab"][aria-selected="true"]`,
  section: SECTION,
  below: `${SECTION} + section`,
};
const ARCHIVED_PAGE = /\/api\/inbox\/archived\/page/;
const SLOW_MS = 600;
const SWITCHES = [
  { to: "snoozed" },
  { to: "archived", slowMs: SLOW_MS },
  { to: "inbox" },
  { to: "archived" },
  { to: "inbox" },
];

const list = (response) => (Array.isArray(response) ? response : (response?.items ?? []));

async function seed(api, log) {
  const snoozed = list(await api("GET", "/api/inbox/snoozed"));
  const archived = list(await api("GET", "/api/inbox/archived/page?limit=5"));
  const inbox = list(await api("GET", "/api/inbox")).filter((item) => !item.archived);
  const byIssue = [...new Map(inbox.map((item) => [item.issue_id, item])).values()];
  if (byIssue.length < 6) throw new Error("the Inbox needs the sidebar fixture: make env-exec ARGS=\"-- node scripts/dev-fixtures.mjs sidebar\"");
  const until = new Date(Date.now() + 3 * 24 * 3600 * 1000).toISOString();
  for (const item of byIssue.slice(0, Math.max(0, 2 - snoozed.length))) await api("POST", `/api/inbox/${item.id}/snooze`, { until });
  for (const item of byIssue.slice(2, 2 + Math.max(0, 3 - archived.length))) await api("POST", `/api/inbox/${item.id}/archive`);
  log(`seeded: ${Math.max(snoozed.length, 2)} snoozed, ${Math.max(archived.length, 3)} archived`);
}

async function openWorkPanel(open, options) {
  const page = await open("/issues", options);
  await page.waitForSelector(`${TARGETS.tab}`, { timeout: 60000 });
  await page.waitForSelector(`${SECTION} [data-inbox-row]`, { timeout: 30000 });
  await page.waitForTimeout(1500); // fonts, queries, entry transitions
  return page;
}

const clickTab = (page, to) => page.click(`${SECTION} [data-inbox-tab="${to}"]`);

export default async function inboxTabs({ open, sample, record, delay, check, checks, api, args, log }) {
  const at = args.indexOf("--locale");
  const locale = at >= 0 ? args[at + 1] : "en-US";
  const { track, landsOn, monotonic, animated, instant } = checks;
  await seed(api, log);

  for (const reducedMotion of [false, true]) {
    const pass = reducedMotion ? "reduced motion" : "motion";
    const page = await openWorkPanel(open, { reducedMotion, locale });
    let from = "inbox";
    for (const { to, slowMs } of SWITCHES) {
      if (slowMs) await delay(page, ARCHIVED_PAGE, slowMs);
      const label = `${pass}, ${from} → ${to}${slowMs ? ` (first load +${slowMs}ms)` : ""}`;
      const frames = await sample(page, label, TARGETS, () => clickTab(page, to), { quietMs: (slowMs ?? 0) + 400 });
      const x = track(frames, "indicator", "x");
      const right = track(frames, "indicator", "right");
      const height = track(frames, "section", "h");
      const below = track(frames, "below", "y");
      check(`${label}: highlight lands on the open tab`, landsOn(frames, "indicator", "tab"));
      if (reducedMotion) {
        check(`${label}: highlight moves instantly`, instant(x));
        check(`${label}: section height changes instantly`, instant(height));
        check(`${label}: section below moves instantly`, instant(below));
      } else {
        check(`${label}: highlight slides`, animated(x));
        check(`${label}: highlight left edge never overshoots`, monotonic(x));
        check(`${label}: highlight right edge never overshoots`, monotonic(right));
        check(`${label}: section height changes one way`, monotonic(height));
        if (Math.abs(height.at(-1) - height[0]) > 1) check(`${label}: section height eases`, animated(height));
        check(`${label}: section below never jumps back`, monotonic(below));
      }
      from = to;
    }
  }

  // The same switches, recorded: a fresh context, so Archived loads cold again.
  const page = await openWorkPanel(open, { locale });
  const box = await page.locator(SECTION).boundingBox();
  const clip = { x: box.x - 8, y: box.y - 8, width: box.width + 16, height: box.height + 120 };
  await record(page, "inbox-tabs", clip, async () => {
    for (const { to, slowMs } of SWITCHES) {
      if (slowMs) await delay(page, ARCHIVED_PAGE, slowMs);
      await clickTab(page, to);
      await page.waitForTimeout((slowMs ?? 0) + 900);
    }
  });
}
