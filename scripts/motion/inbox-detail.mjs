// Scenario for scripts/motion/capture.mjs: archiving from the Inbox detail,
// which advances to the next notification and slides its issue in
// (.animate-inbox-enter-next). It needs the `sidebar` fixture:
//
//   make env-exec ARGS="-- node scripts/dev-fixtures.mjs sidebar"
//   make env-exec ARGS="-- node scripts/motion/capture.mjs scripts/motion/inbox-detail.mjs --out ../motion"
//
// Each pass archives two notifications, so one run uses six; run the fixture
// again for another. The reduced passes cover both sources: the system
// setting, and the Appearance page's Reduce motion switch.

const TARGETS = {
  column: '[data-tab-scroll-root^="main:"] > div',
  bar: "[data-inbox-bar]",
};
const ARCHIVES = 2;
const PASSES = [
  { name: "motion", options: {} },
  { name: "system reduced motion", options: { reducedMotion: true }, reduced: true },
  {
    name: "Appearance Reduce motion",
    options: { storage: { multica_appearance: JSON.stringify({ state: { reduceMotion: true }, version: 0 }) } },
    reduced: true,
  },
];

// Boxes cannot show a fade, which is what reduced motion used to fall back
// to, so the column's opacity is read every frame too (as a percentage).
function watchOpacity(page) {
  return page.evaluate((selector) => {
    const state = { values: [], stopped: false };
    window.__motionOpacity = state;
    const frame = () => {
      if (state.stopped) return;
      const el = document.querySelector(selector);
      if (el) state.values.push(Number(getComputedStyle(el).opacity) * 100);
      requestAnimationFrame(frame);
    };
    requestAnimationFrame(frame);
  }, TARGETS.column);
}
const stopOpacity = (page) =>
  page.evaluate(() => {
    window.__motionOpacity.stopped = true;
    return window.__motionOpacity.values;
  });

async function openInbox(open, options) {
  const page = await open("/inbox", options);
  const row = page.locator("[data-inbox-row]").first();
  await row.waitFor({ timeout: 60000 });
  await row.click();
  await page.waitForSelector(`${TARGETS.bar} button`, { timeout: 30000 });
  await page.waitForTimeout(1500); // fonts, queries, the first entry
  return page;
}

export default async function inboxDetail({ open, sample, check, checks }) {
  const { track, animated, instant } = checks;
  for (const pass of PASSES) {
    const page = await openInbox(open, pass.options);
    for (let i = 1; i <= ARCHIVES; i++) {
      const label = `${pass.name}, archive ${i} → next notification`;
      const before = page.url();
      const archive = page.locator(TARGETS.bar).getByRole("button", { name: /^Archive/ });
      await watchOpacity(page);
      const frames = await sample(page, label, TARGETS, () => archive.click(), { quietMs: 600 });
      const opacity = await stopOpacity(page);
      await page.waitForURL((url) => url.href !== before, { timeout: 5000 }).catch(() => {});
      check(`${label}: opens the next issue`, { ok: page.url() !== before, detail: page.url().split("?")[1] ?? page.url() });
      const y = track(frames, "column", "y");
      if (pass.reduced) {
        check(`${label}: issue lands without moving`, instant(y));
        check(`${label}: issue appears without a fade`, instant(opacity));
      } else {
        check(`${label}: issue slides in`, animated(y));
        check(`${label}: issue fades in`, animated(opacity));
      }
      await page.waitForTimeout(800);
    }
  }
}
