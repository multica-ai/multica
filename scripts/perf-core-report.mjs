#!/usr/bin/env node
/**
 * Turn core-path benchmark reports (e2e/perf/core-paths.spec.ts, MUL-9539)
 * and Desktop resource reports (e2e/perf/desktop-resources.spec.ts, MUL-9612)
 * into Markdown tables, one column per report:
 *
 *   node scripts/perf-core-report.mjs before=perf/a.json after=perf/b.json
 *   node scripts/perf-core-report.mjs web=perf/web.json \
 *     desktop=perf/desktop/core-paths.json desktop=perf/desktop/desktop-resources.json
 *
 * Each report lands in the tables of its own kind, so one label can name a
 * core-path report and a resource report from the same run. Reports measured
 * against different datasets are refused: their numbers do not describe the
 * same work. `scripts/perf-compare.mjs --scenario core-paths` builds and
 * measures two refs itself and renders its summary with this module.
 */
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ISSUE_ROWS = [
  ["open_content_ms", "Open → first comments visible (ms)"],
  ["open_stalls", "Open: stalls >50ms"],
  ["open_stall_max_ms", "Open: longest stall (ms)"],
  ["scroll_stalls", "Scroll to end: stalls >50ms"],
  ["scroll_stall_max_ms", "Scroll to end: longest stall (ms)"],
  ...["left", "right"].flatMap((side) => [
    [`${side}_toggle_next_paint_ms`, `${side === "left" ? "Left nav" : "Right sidebar"} toggle → next paint, median (ms)`],
    [`${side}_toggle_task_ms`, "… main-thread work per toggle, median (ms)"],
    [`${side}_toggle_stalls`, "… stalls >50ms in 10 toggles"],
    [`${side}_toggle_stall_max_ms`, "… longest stall (ms)"],
  ]),
];
const INBOX_ROWS = [
  ["ready_median_ms", "Click → content visible, median (ms)"],
  ["ready_p95_ms", "… p95 (ms)"],
  ["ready_max_ms", "… worst (ms)"],
  ["next_paint_median_ms", "Click → next paint, median (ms)"],
  ["next_paint_max_ms", "Click → next paint, worst (ms)"],
  ["stalls_per_switch", "Stalls >50ms per switch"],
  ["stall_max_ms", "Longest stall (ms)"],
  ["freezes", "Freezes >200ms (all switches)"],
];
const IDLE_ROWS = [
  ["cpu_pct.app.mean", "App CPU, mean (% of one core)"],
  ["cpu_pct.app.p95", "… p95 of 1 s samples"],
  ["cpu_pct.renderer.mean", "Renderer CPU, mean"],
  ["cpu_pct.gpu.mean", "GPU process CPU, mean"],
  ["cpu_pct.main.mean", "Main process CPU, mean"],
  ["cpu_pct.utility.mean", "Utility processes CPU, mean"],
  ["renderer_main_thread_busy_pct", "Renderer main thread busy (%)"],
  ["stalls.stalls", "Stalls >50ms while idle"],
  ["rss_mb.app.end", "App memory (RSS) at the end (MB)"],
];
const MEMORY_ROWS = [
  ["switches", "Inbox switches"],
  ["rss_mb.app.first_minute", "App RSS, first minute (MB)"],
  ["rss_mb.app.last_minute", "App RSS, last minute (MB)"],
  ["rss_mb.app.slope_mb_per_min", "App RSS growth (MB/min)"],
  ["rss_mb.app.slope_second_half_mb_per_min", "… second half only (MB/min)"],
  ["rss_mb.renderer.slope_mb_per_min", "Renderer RSS growth (MB/min)"],
  ["js_heap_mb_end", "JS heap at the end (MB)"],
  ["js_heap_mb_after_gc", "… after GC (MB)"],
  ["switch_ready_ms.first_40_median", "Switch → content, first 40, median (ms)"],
  ["switch_ready_ms.last_40_median", "Switch → content, last 40, median (ms)"],
];

export class DatasetMismatchError extends Error {}

const get = (object, path) => path.split(".").reduce((v, k) => v?.[k], object);
const fmt = (v) => (typeof v === "number" ? (Number.isInteger(v) ? String(v) : v.toFixed(1)) : "—");
const header = (group) => `| | ${group.map((i) => i.label).join(" | ")} |\n|---|${group.map(() => "---:").join("|")}|`;

/**
 * Renders reports side by side. Each input is `{ label, report }`; throws
 * DatasetMismatchError when the reports were measured on different datasets.
 * Reports without a `kind` (all of them before MUL-9612) are web core-path reports.
 */
export function renderCoreReport(rawInputs) {
  const inputs = rawInputs.map(({ label, report }) => ({ label, report: { kind: "core-paths", platform: "web", ...report } }));
  const digests = new Set(inputs.map((i) => i.report.dataset?.digest));
  if (digests.size > 1) {
    throw new DatasetMismatchError(`reports use different datasets: ${[...digests].join(", ")}`);
  }
  const lines = [];

  const core = inputs.filter((i) => i.report.kind === "core-paths");
  const issueKeys = [...new Set(core.flatMap((i) => Object.keys(i.report.summary?.issue ?? {})))];
  for (const key of issueKeys) {
    lines.push(`\n**Open a big issue: \`${key}\`** (median of samples)\n`, header(core));
    for (const [field, name] of ISSUE_ROWS) {
      lines.push(`| ${name} | ${core.map((i) => fmt(i.report.summary?.issue?.[key]?.[field])).join(" | ")} |`);
    }
  }
  for (const phase of ["first-visit", "revisit"]) {
    if (!core.some((i) => i.report.summary?.inbox?.[phase])) continue;
    lines.push(`\n**Inbox switching: ${phase}** (all switches across samples)\n`, header(core));
    for (const [field, name] of INBOX_ROWS) {
      lines.push(`| ${name} | ${core.map((i) => fmt(i.report.summary?.inbox?.[phase]?.[field])).join(" | ")} |`);
    }
  }
  if (core.some((i) => i.report.summary?.inbox?.heap_mb_after_gc)) {
    lines.push(`| JS heap after 30 switches, after GC (MB) | ${core.map((i) => fmt(i.report.summary?.inbox?.heap_mb_after_gc)).join(" | ")} |`);
  }

  const resources = inputs.filter((i) => i.report.kind === "desktop-resources");
  for (const [section, title, rows] of [
    ["idle", (r) => `**Desktop idle on the big issue** (${fmt(r.idle.seconds)} s, sampled every second from outside the app)`, IDLE_ROWS],
    ["memory", (r) => `**Desktop memory while switching Inbox rows** (${fmt(r.memory.minutes)} min, sampled every 5 s)`, MEMORY_ROWS],
  ]) {
    const group = resources.filter((i) => i.report[section]);
    if (group.length === 0) continue;
    lines.push(`\n${title(group[0].report)}\n`, header(group));
    for (const [field, name] of rows) {
      lines.push(`| ${name} | ${group.map((i) => fmt(get(i.report[section], field))).join(" | ")} |`);
    }
  }

  const first = inputs.find((i) => i.report.dataset && i.report.environment)?.report;
  if (first) {
    lines.push(
      "",
      `Dataset ${first.dataset.version} (${first.dataset.digest}): ${Object.entries(first.dataset.counts ?? {}).map(([k, v]) => `${v} ${k}`).join(", ")}.`,
    );
  }
  for (const { label, report } of inputs) {
    const e = report.environment;
    if (!e) {
      lines.push(`${label}: ref ${report.ref ?? "?"}, no environment recorded.`);
      continue;
    }
    const until = e.inbox_until === "title" ? "issue title is visible" : "notified comment is in view";
    const build = report.build ? ` (${report.build} build)` : "";
    lines.push(`${label} — ${report.kind}, ${report.platform}${build}: ref ${report.ref ?? "?"}, Chromium ${e.browser}, ${e.viewport}, ${e.cpu} × ${e.cores}` +
      (report.kind === "core-paths" ? `, CPU ${e.cpu_throttle}×, ${e.repeats} samples` : "") +
      (report.summary?.inbox?.["first-visit"] ? `, ${e.inbox_rows} Inbox rows, switch ends when the ${until}` : "") +
      (typeof e.load_1m_end === "number" ? `, load ${fmt(Math.round(e.load_1m_end * 10) / 10)} at the end` : "") + ".");
  }
  return lines.join("\n");
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const inputs = process.argv.slice(2).map((arg) => {
    const at = arg.indexOf("=");
    const label = at > 0 ? arg.slice(0, at) : arg;
    const path = at > 0 ? arg.slice(at + 1) : arg;
    return { label, report: JSON.parse(readFileSync(path, "utf8")) };
  });
  if (inputs.length === 0) {
    console.error("usage: node scripts/perf-core-report.mjs <label>=<report.json> [...]");
    process.exit(2);
  }
  try {
    console.log(renderCoreReport(inputs));
  } catch (error) {
    if (!(error instanceof DatasetMismatchError)) throw error;
    console.error(error.message);
    process.exit(1);
  }
}
