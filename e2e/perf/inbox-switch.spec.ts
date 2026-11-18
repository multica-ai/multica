/**
 * Switching between issues in the Inbox (MUL-9539 scenario 2). The Inbox lives
 * in the Work panel: its section shows the newest rows and expands to all of
 * them. Both states are measured:
 *
 *   collapsed — the default five rows: walk down them and back up.
 *   expanded  — after "Show N more": walk ten rows down and back up.
 *
 * For each click: time until the selected issue is on screen, time until the
 * comment the row links to is in view (rows that link to one, on the walk
 * down), and the stalls from the click until the user would click again. The walk
 * down opens issues for the first time; the walk back opens cached ones.
 * Real API and database seeded by scripts/perf-seed.mjs; opt in with
 * PERF_SCENARIO=inbox-switch and PERF_MANIFEST=<manifest>.
 */
import { test, expect, type Page } from "@playwright/test";
import pg from "pg";
import {
  inputAt, inputCount, loadManifest, median, newMeasuredContext, now, percentile, repeats, round,
  signIn, stallsBetween, throttle, waitFor, waitForQuiet, writeReport, type ManifestInboxRow,
} from "./lib/probe";

const MODES = { collapsed: 5, expanded: 10 } as const;
type Mode = keyof typeof MODES;
/** How long the user reads before the next click; work still running then is felt as lag. */
const DWELL_MS = 1000;
/** A switch that has not shown its content by now is recorded as missed, not as a time. */
const SWITCH_TIMEOUT_MS = 15_000;

interface SwitchSample {
  mode: Mode;
  round: number;
  pass: "first" | "back";
  row: number;
  kind: string;
  issue_visible_ms: number | null;
  comment_visible_ms: number | null;
  jumps_to_comment: boolean;
  stalls: Awaited<ReturnType<typeof stallsBetween>>;
}

test("inbox: switch between issues", async ({ browser }) => {
  test.skip(process.env.PERF_SCENARIO !== "inbox-switch", "Requires a seeded local API and a production build");
  test.setTimeout(3_600_000);
  const manifest = loadManifest();
  const modes = (process.env.PERF_INBOX_MODES ?? "collapsed,expanded").split(",") as Mode[];
  const auth = await signIn(browser, manifest.user.email);
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  const samples: SwitchSample[] = [];
  const expansions: Record<string, unknown>[] = [];
  try {
    for (const mode of modes) {
      const rows = manifest.inbox.slice(0, MODES[mode]);
      const walk = [
        ...rows.map((row, i) => ({ row, i, pass: "first" as const })),
        ...rows.slice(0, -1).reverse().map((row, i) => ({ row, i: rows.length - 2 - i, pass: "back" as const })),
      ];
      for (let round_ = -1; round_ < repeats(); round_++) {
        // Opening a row marks it read; every round starts from the same unread Inbox.
        await db.query("UPDATE inbox_item SET read = false WHERE id = ANY($1::uuid[])", [manifest.inbox.map((row) => row.inbox_item_id)]);
        const context = await newMeasuredContext(browser, auth);
        try {
          const page = await context.newPage();
          const errors: string[] = [];
          page.on("pageerror", (error) => errors.push(error.message));
          await throttle(page);
          await page.goto(`/${manifest.workspace.slug}/inbox`);
          const section = page.locator('[data-mc="panel-section"]').filter({ has: page.locator('[data-mc="nav-item"]').filter({ hasText: rows[0]!.title }) });
          const list = section.locator('[data-mc="nav-item"]');
          await expect(list.filter({ hasText: rows[0]!.title }).first()).toBeVisible({ timeout: 60_000 });
          await waitForQuiet(page, 0, 1500);

          if (mode === "expanded") {
            const before = await inputCount(page);
            await section.getByText(/^Show \d+ more/).first().click();
            await expect(list.filter({ hasText: rows[rows.length - 1]!.title }).first()).toBeAttached();
            const input = await inputAt(page, before);
            const settled = await waitForQuiet(page, input, 1000);
            if (round_ >= 0) {
              expansions.push({ round: round_, rows: await list.count(), settled_ms: round(settled - input), stalls: await stallsBetween(page, input, settled + 1) });
            }
          }

          for (const step of walk) {
            // Going back to an issue restores where the user left it rather
            // than landing on the comment again, so only the walk down waits
            // for the comment.
            const sample = await switchTo(page, list, step.row, step.pass === "first");
            if (round_ >= 0) samples.push({ mode, round: round_, pass: step.pass, row: step.i, kind: step.row.kind, ...sample });
          }
          expect(errors).toEqual([]);
          if (round_ >= 0) {
            const of = samples.filter((s) => s.mode === mode && s.round === round_);
            console.log(JSON.stringify({ mode, round: round_, issue_visible_ms_median: round(median(values(of, "issue_visible_ms"))), comment_visible_ms_median: round(median(values(of, "comment_visible_ms"))) }));
          }
        } finally {
          await context.close();
        }
      }
    }
  } finally {
    await db.end();
  }

  const summarize = (of: SwitchSample[]) => ({
    switches: of.length,
    issue_visible_ms_median: round(median(values(of, "issue_visible_ms"))),
    issue_visible_ms_p95: round(percentile(values(of, "issue_visible_ms"), 95)),
    comment_visible_ms_median: round(median(values(of, "comment_visible_ms"))),
    comment_visible_ms_p95: round(percentile(values(of, "comment_visible_ms"), 95)),
    comment_visible_ms_max: round(Math.max(...values(of, "comment_visible_ms"))),
    issue_missed: of.filter((s) => s.issue_visible_ms === null).length,
    comment_missed: of.filter((s) => s.jumps_to_comment && s.comment_visible_ms === null).length,
    longest_stall_ms_median: round(median(of.map((s) => s.stalls.longest_ms))),
    longest_stall_ms_max: round(Math.max(...of.map((s) => s.stalls.longest_ms))),
    long_frame_ms_median: round(median(of.map((s) => s.stalls.long_frame_ms))),
    stalls_per_switch: round(of.reduce((sum, s) => sum + s.stalls.stalls, 0) / Math.max(1, of.length), 2),
  });
  const summary = Object.fromEntries(modes.map((mode) => {
    const of = samples.filter((s) => s.mode === mode);
    return [mode, {
      first_pass: summarize(of.filter((s) => s.pass === "first")),
      back_pass: summarize(of.filter((s) => s.pass === "back")),
      by_kind_first_pass: Object.fromEntries(["big", "xl", "p99", "mid", "small"]
        .map((kind) => [kind, of.filter((s) => s.kind === kind && s.pass === "first")] as const)
        .filter(([, list]) => list.length > 0)
        .map(([kind, list]) => [kind, summarize(list)])),
      ...(mode === "expanded" ? { expand: { settled_ms_median: round(median(expansions.map((e) => e.settled_ms as number))), samples: expansions } } : {}),
    }];
  }));
  writeReport("inbox-switch", browser, manifest, samples, summary);
});

function values(of: SwitchSample[], key: "issue_visible_ms" | "comment_visible_ms"): number[] {
  return of.map((s) => s[key]).filter((v): v is number => v !== null);
}

async function switchTo(page: Page, list: ReturnType<Page["locator"]>, row: ManifestInboxRow, landsOnComment: boolean) {
  const item = list.filter({ hasText: row.title }).first();
  await item.scrollIntoViewIfNeeded();
  // The row links to the comment the app will land on — the oldest unread one
  // on the issue, which is not necessarily the notification's own comment.
  const href = (await item.locator("a[href]").first().getAttribute("href")) ?? "";
  const target = landsOnComment ? (/#comment-([0-9a-f-]{36})/.exec(href)?.[1] ?? null) : null;
  const before = await inputCount(page);
  const issue = waitFor(page, "issueReady", { title: row.title, expectComments: row.comments > 0 }, SWITCH_TIMEOUT_MS).catch(() => null);
  const comment = target
    ? waitFor(page, "commentInView", { title: row.title, commentId: target }, SWITCH_TIMEOUT_MS).catch(() => null)
    : Promise.resolve(null);
  await item.click();
  const [issueAt, commentAt] = await Promise.all([issue, comment]);
  const input = await inputAt(page, before);
  await page.waitForTimeout(DWELL_MS);
  const end = await now(page);
  return {
    issue_visible_ms: issueAt === null ? null : round(issueAt - input),
    comment_visible_ms: commentAt === null ? null : round(commentAt - input),
    jumps_to_comment: !!target,
    stalls: await stallsBetween(page, input, end),
  };
}
