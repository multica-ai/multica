#!/usr/bin/env node
// Verifies the repository documentation standard described in docs/AGENTS.md.
//
//   node scripts/verify-docs.mjs
//
// Five checks, all mechanical:
//
//   1. registry  — every governed Markdown file is in scripts/docs.manifest.json,
//                  and every manifest entry still exists on disk.
//   2. budgets   — no governed document exceeds its word ceiling.
//   3. status    — no implementation-status metadata outside docs/decisions/.
//   4. links     — every relative Markdown link resolves to a real path.
//   5. decisions — every decision record matches the format in
//                  docs/decisions/README.md.
//
// Exits non-zero with one line per problem.

import { readFileSync, readdirSync, statSync, existsSync } from "node:fs";
import { join, dirname, relative, resolve, posix } from "node:path";
import { fileURLToPath } from "node:url";

const REPO_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const MANIFEST_PATH = "scripts/docs.manifest.json";
const DECISIONS_DIR = "docs/decisions";

const LIFECYCLES = ["proposed", "implemented", "rejected"];
const CLASSES = ["feature", "bug-fix", "simplification", "architecture", "process", "testing"];
const RECORD_FILENAME = /^\d{4}-\d{2}-\d{2}-[a-z0-9]+(?:-[a-z0-9]+)*\.md$/;
const ALTERNATIVES_ESCAPE = "<!-- decision-format: alternatives-not-recorded (pre-format record) -->";

// Headings that only make sense while work is unbuilt. In an implemented
// record they are spec-speak: the work shipped, so say what it does.
const PROPOSAL_ONLY_HEADINGS = ["## Proposal", "## Plan", "## Migration plan", "## Acceptance criteria"];

// Line-anchored metadata that pins a document to a moment in time. The
// lifecycle folder a decision record sits in carries status instead, so
// moving the file is the only way to change it and it cannot go stale in place.
const STATUS_MARKERS = [
  { pattern: /^\s*>?\s*\**\s*status\s*\**\s*:/i, label: "a `Status:` line" },
  { pattern: /^\s*>?\s*\**\s*last updated\s*\**\s*:/i, label: "a `Last updated:` line" },
  { pattern: /^\s*>?\s*\**\s*owner\s*\**\s*:\s*TBD/i, label: "an `Owner: TBD` line" },
  { pattern: /^\s*[-*]\s*\[[ xX]\]\s/, label: "a checkbox task list" },
];

const problems = [];
const fail = (file, message) => problems.push(`${file}: ${message}`);

const read = (relPath) => readFileSync(join(REPO_ROOT, relPath), "utf8");
const wordCount = (text) => text.split(/\s+/).filter(Boolean).length;

function walk(relDir, predicate) {
  const absDir = join(REPO_ROOT, relDir);
  if (!existsSync(absDir)) return [];
  const out = [];
  for (const entry of readdirSync(absDir)) {
    const relPath = posix.join(relDir, entry);
    if (statSync(join(REPO_ROOT, relPath)).isDirectory()) out.push(...walk(relPath, predicate));
    else if (predicate(relPath)) out.push(relPath);
  }
  return out;
}

const isMarkdown = (p) => p.endsWith(".md");
const isDecisionRecord = (p) => p.startsWith(`${DECISIONS_DIR}/`) && p !== `${DECISIONS_DIR}/README.md`;

// A document is governed when it sits in a tier the manifest tracks: any
// Markdown at the repository root, or anywhere under docs/ except the
// decision tree, whose files are governed by their own format rules instead.
function governedCandidates() {
  const rootDocs = readdirSync(REPO_ROOT)
    .filter((name) => isMarkdown(name) && statSync(join(REPO_ROOT, name)).isFile());
  const docsTree = walk("docs", (p) => isMarkdown(p) && !p.startsWith(`${DECISIONS_DIR}/`));
  return [...rootDocs, ...docsTree].sort();
}

// --- 1 & 2. Registry and budgets ------------------------------------------

const manifest = JSON.parse(read(MANIFEST_PATH));
const budgets = manifest.documents ?? {};

// Auto-discovered tiers must be registered. The manifest may also list
// documents outside them — a subtree CLAUDE.md, the doc site's contract —
// which are then held to the same ceiling and rules.
for (const relPath of governedCandidates()) {
  if (!(relPath in budgets)) {
    fail(relPath, `not registered in ${MANIFEST_PATH}. Add it with a word ceiling, or move it to the tier that owns this content (docs/AGENTS.md).`);
  }
}

const governed = Object.keys(budgets).filter((relPath) => {
  if (existsSync(join(REPO_ROOT, relPath))) return true;
  fail(relPath, `listed in ${MANIFEST_PATH} but missing on disk. Delete the manifest entry if the document is gone.`);
  return false;
});

for (const relPath of governed) {
  const words = wordCount(read(relPath));
  const ceiling = budgets[relPath];
  if (words > ceiling) {
    fail(relPath, `${words} words exceeds its ${ceiling}-word ceiling. Relocate, condense, or justify a higher ceiling in ${MANIFEST_PATH}.`);
  }
}

// --- 3. Status metadata outside the decision tree --------------------------

for (const relPath of governed) {
  if (relPath === "docs/AGENTS.md") continue; // names the banned markers in order to ban them
  read(relPath).split("\n").forEach((line, index) => {
    for (const { pattern, label } of STATUS_MARKERS) {
      if (pattern.test(line)) {
        fail(`${relPath}:${index + 1}`, `${label} outside docs/decisions/. Implementation status belongs to a decision record's lifecycle folder.`);
      }
    }
  });
}

// --- 4. Relative link targets ---------------------------------------------

const linkScan = [...governed, `${DECISIONS_DIR}/README.md`, ...walk(DECISIONS_DIR, isDecisionRecord)];
const LINK = /\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g;

for (const relPath of linkScan) {
  // Fenced blocks and inline code carry link-shaped text that is illustrative,
  // not a reference — `![alt](url)` in a code span is documentation of syntax.
  const body = read(relPath)
    .replace(/^```[\s\S]*?^```/gm, "")
    .replace(/`[^`\n]*`/g, "");
  for (const [, rawTarget] of body.matchAll(LINK)) {
    if (/^(?:[a-z][a-z0-9+.-]*:|\/\/|#)/i.test(rawTarget)) continue; // external, protocol-relative, same-page
    const target = rawTarget.split("#")[0];
    if (!target) continue;
    const resolved = target.startsWith("/")
      ? join(REPO_ROOT, target)
      : resolve(join(REPO_ROOT, dirname(relPath)), target);
    if (!existsSync(resolved)) {
      fail(relPath, `broken link \`${rawTarget}\` — no such path ${relative(REPO_ROOT, resolved)}`);
    }
  }
}

// --- 5. Decision record format --------------------------------------------

for (const relPath of walk(DECISIONS_DIR, isDecisionRecord)) {
  const [lifecycle, className, ...rest] = relPath.slice(DECISIONS_DIR.length + 1).split("/");
  const filename = rest[rest.length - 1];

  if (!LIFECYCLES.includes(lifecycle)) {
    fail(relPath, `\`${lifecycle}/\` is not a lifecycle folder. Use one of: ${LIFECYCLES.join(", ")}.`);
    continue;
  }
  if (!CLASSES.includes(className)) {
    fail(relPath, `\`${className}/\` is not a class folder. Use one of: ${CLASSES.join(", ")}.`);
    continue;
  }
  if (rest.length !== 1) {
    fail(relPath, "records are exactly two folders deep: {lifecycle}/{class}/yyyy-mm-dd-topic-title.md");
    continue;
  }
  if (!RECORD_FILENAME.test(filename)) {
    fail(relPath, "filename must be yyyy-mm-dd-lowercase-topic-title.md, dated when the topic was first proposed.");
  }

  const body = read(relPath);
  const lines = body.split("\n");

  if (!/^# Decision: \S/.test(lines[0] ?? "")) {
    fail(relPath, "line 1 must be `# Decision: <title>`.");
  }
  if ((lines[1] ?? "").trim() !== "") {
    fail(relPath, "line 2 must be blank.");
  }

  const statusLine = lines[2] ?? "";
  const status = statusLine.startsWith("Status: ") ? statusLine.slice("Status: ".length).trim() : null;
  if (status === null) {
    fail(relPath, "line 3 must be `Status: <status>`.");
  } else if (lifecycle === "rejected") {
    if (!/^rejected — \S/.test(status)) {
      fail(relPath, "a rejected record's status must read `Status: rejected — <why, in one line>`.");
    }
  } else if (status !== lifecycle) {
    fail(relPath, `status \`${status}\` disagrees with the \`${lifecycle}/\` folder it lives in.`);
  }

  const headings = lines.filter((line) => line.startsWith("## ")).map((line) => line.trim());
  if (headings[0] !== "## Problem") {
    fail(relPath, `the body must open with \`## Problem\` (found \`${headings[0] ?? "no heading"}\`).`);
  }
  if (!headings.includes("## Alternatives considered") && !body.includes(ALTERNATIVES_ESCAPE)) {
    fail(relPath, "missing `## Alternatives considered`. A decision recorded without what it beat gets re-litigated.");
  }

  if (lifecycle === "implemented") {
    if (!headings.includes("## Decision")) {
      fail(relPath, "an implemented record needs a present-tense `## Decision` section.");
    }
    for (const heading of PROPOSAL_ONLY_HEADINGS) {
      if (headings.includes(heading)) {
        fail(relPath, `\`${heading}\` is proposal-era spec-speak in an implemented record. State what shipped instead.`);
      }
    }
  }
  if (lifecycle === "proposed" && !headings.includes("## Proposal")) {
    fail(relPath, "a proposed record needs a `## Proposal` section.");
  }
}

// --- Report ----------------------------------------------------------------

if (problems.length > 0) {
  console.error(`docs:check found ${problems.length} problem${problems.length === 1 ? "" : "s"}:\n`);
  for (const problem of problems.sort()) console.error(`  ${problem}`);
  console.error(`\nThe rules live in docs/AGENTS.md and docs/decisions/README.md.`);
  process.exit(1);
}

const recordCount = walk(DECISIONS_DIR, isDecisionRecord).length;
console.log(`docs:check passed — ${governed.length} governed documents, ${recordCount} decision records.`);
