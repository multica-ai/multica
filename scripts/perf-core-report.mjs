#!/usr/bin/env node
/**
 * Turn core-path benchmark reports (e2e/perf/core-paths.spec.ts, MUL-9539)
 * into one Markdown table, one column per report:
 *
 *   node scripts/perf-core-report.mjs before=perf/a.json after=perf/b.json
 *
 * Reports measured against different datasets are refused: their numbers do
 * not describe the same work.
 */
import { readFileSync } from "node:fs";

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
const digests = new Set(inputs.map((i) => i.report.dataset?.digest));
if (digests.size > 1) {
  console.error(`reports use different datasets: ${[...digests].join(", ")}`);
  process.exit(1);
}

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

const fmt = (v) => (typeof v === "number" ? (Number.isInteger(v) ? String(v) : v.toFixed(1)) : "—");
const header = `| | ${inputs.map((i) => i.label).join(" | ")} |\n|---|${inputs.map(() => "---:").join("|")}|`;
const lines = [];

const issueKeys = [...new Set(inputs.flatMap((i) => Object.keys(i.report.summary?.issue ?? {})))];
for (const key of issueKeys) {
  lines.push(`\n**Open a big issue: \`${key}\`** (median of samples)\n`, header);
  for (const [field, name] of ISSUE_ROWS) {
    lines.push(`| ${name} | ${inputs.map((i) => fmt(i.report.summary.issue[key]?.[field])).join(" | ")} |`);
  }
}
for (const phase of ["first-visit", "revisit"]) {
  if (!inputs.some((i) => i.report.summary?.inbox?.[phase])) continue;
  lines.push(`\n**Inbox switching: ${phase}** (all switches across samples)\n`, header);
  for (const [field, name] of INBOX_ROWS) {
    lines.push(`| ${name} | ${inputs.map((i) => fmt(i.report.summary.inbox[phase]?.[field])).join(" | ")} |`);
  }
}
if (inputs.some((i) => i.report.summary?.inbox?.heap_mb_after_gc)) {
  lines.push(`| JS heap after 30 switches, after GC (MB) | ${inputs.map((i) => fmt(i.report.summary.inbox.heap_mb_after_gc)).join(" | ")} |`);
}
const env = inputs[0].report;
lines.push(
  "",
  `Dataset ${env.dataset.version} (${env.dataset.digest}): ${Object.entries(env.dataset.counts).map(([k, v]) => `${v} ${k}`).join(", ")}.`,
  `Chromium ${env.environment.browser}, ${env.environment.viewport}, ${env.environment.cpu} × ${env.environment.cores}.`,
  ...inputs.map(({ label, report }) => {
    const e = report.environment;
    const until = e.inbox_until === "title" ? "issue title is visible" : "notified comment is in view";
    return `${label}: ref ${report.ref ?? "?"}, CPU ${e.cpu_throttle}×, ${e.repeats} samples` +
      (report.summary?.inbox?.["first-visit"] ? `, ${e.inbox_rows} Inbox rows, switch ends when the ${until}` : "") + ".";
  }),
);
console.log(lines.join("\n"));
