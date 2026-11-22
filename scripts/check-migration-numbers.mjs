#!/usr/bin/env node
// Fails when a branch adds a migration whose numeric prefix is already taken
// on the base branch, or by another migration the same branch adds.
//
// TestMigrationNumericPrefixesAreUnique checks the same rule, but a PR's CI
// runs it only against the main of the moment it was pushed. When main later
// gains a migration with the same number there is no textual conflict, the
// PR stays green, and merging it turns main red (MUL-9832). So run this
// against a freshly fetched main right before merging. The migration-numbers
// workflow also re-runs it for every open PR whenever main gains a migration
// and posts the result as a commit status on each PR head.
//
// The check models the merge result: the base's migrations, minus the ones
// the branch deleted since it forked, plus the ones it added.
//
// Usage:
//   git fetch origin main
//   node scripts/check-migration-numbers.mjs [--base origin/main] [--head HEAD] [--json]
//
// Exit codes: 0 no collision, 1 collision, 2 the check could not run.

import { execFileSync } from "node:child_process";
import { pathToFileURL } from "node:url";

const MIGRATIONS_DIR = "server/migrations";
// Mirrors firstUniqueMigrationNumber in migrations_lint_test.go: earlier
// numbers carry historical duplicates.
const FIRST_UNIQUE_MIGRATION_NUMBER = 129;

export function migrationNumber(stem) {
  const match = /^(\d+)_/.exec(stem);
  return match ? Number(match[1]) : null;
}

// Each argument is the list of migration stems (file names without
// `.up.sql`) present at that commit.
export function findMigrationCollisions({ base, head, mergeBase }) {
  const headStems = new Set(head);
  const mergeBaseStems = new Set(mergeBase);
  const added = [...headStems].filter((stem) => !mergeBaseStems.has(stem)).sort();
  const addedStems = new Set(added);
  const deleted = new Set([...mergeBaseStems].filter((stem) => !headStems.has(stem)));
  const merged = new Set([...base.filter((stem) => !deleted.has(stem)), ...added]);

  const stemsByNumber = new Map();
  for (const stem of merged) {
    const number = migrationNumber(stem);
    if (number === null) continue;
    stemsByNumber.set(number, [...(stemsByNumber.get(number) ?? []), stem]);
  }

  const collisions = [];
  for (const stem of added) {
    const number = migrationNumber(stem);
    if (number === null || number < FIRST_UNIQUE_MIGRATION_NUMBER) continue;
    const others = stemsByNumber.get(number).filter((other) => other !== stem);
    if (others.length === 0) continue;
    collisions.push({
      stem,
      number,
      conflictsWith: others.sort().map((other) => ({ stem: other, addedByHead: addedStems.has(other) })),
    });
  }

  let next = Math.max(0, ...stemsByNumber.keys());
  for (const collision of collisions) collision.suggestedNumber = ++next;

  return { added, collisions };
}

export function checkRefs({ base, head, cwd }) {
  const git = (...args) => execFileSync("git", args, { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  const stemsAt = (ref) =>
    git("ls-tree", "--name-only", ref, "--", `${MIGRATIONS_DIR}/`)
      .split("\n")
      .filter((path) => path.endsWith(".up.sql"))
      .map((path) => path.slice(MIGRATIONS_DIR.length + 1, -".up.sql".length));
  const mergeBase = git("merge-base", base, head).trim();
  return findMigrationCollisions({ base: stemsAt(base), head: stemsAt(head), mergeBase: stemsAt(mergeBase) });
}

// Leaves out the suggested number on purpose: it moves with every migration
// main gains, and the open-PR sweep reposts a status only when this text
// changes.
export function summarize(collisions, baseLabel) {
  if (collisions.length === 0) return `Migration numbers are free on ${baseLabel}`;
  const numbers = [...new Set(collisions.map((collision) => collision.number))];
  const verb = numbers.length === 1 ? "is" : "are";
  return `Migration ${numbers.join(", ")} ${verb} already taken; renumber above the highest on ${baseLabel}`;
}

function main(argv) {
  const base = readFlag(argv, "--base") ?? "origin/main";
  const head = readFlag(argv, "--head") ?? "HEAD";
  const json = argv.includes("--json");

  let result;
  try {
    result = checkRefs({ base, head });
  } catch (error) {
    console.error(`Could not compare migrations between ${base} and ${head}: ${error.stderr?.trim() || error.message}`);
    return 2;
  }
  const { added, collisions } = result;

  if (json) {
    console.log(JSON.stringify({ base, head, added, collisions, summary: summarize(collisions, base) }));
    return collisions.length === 0 ? 0 : 1;
  }

  if (added.length === 0) {
    console.log(`No migrations added against ${base}.`);
    return 0;
  }
  if (collisions.length === 0) {
    console.log(`${added.length} added migration(s) have numbers that are free on ${base}.`);
    return 0;
  }

  for (const { stem, number, conflictsWith, suggestedNumber } of collisions) {
    const owners = conflictsWith
      .map((other) => (other.addedByHead ? `${other.stem} (also added here)` : other.stem))
      .join(", ");
    const message = `${stem} uses migration number ${number}, already taken by ${owners}; renumber it to ${suggestedNumber}`;
    console.log(process.env.GITHUB_ACTIONS ? `::error file=${MIGRATIONS_DIR}/${stem}.up.sql::${message}` : `  ${message}`);
  }
  console.error(
    [
      "",
      `${collisions.length} migration number(s) collide with ${base}.`,
      "Rename both the .up.sql and .down.sql files (and any references to the",
      "version, such as concurrent index cleanups in server/cmd/migrate/main.go),",
      "then fetch main again and rerun this check before merging.",
    ].join("\n"),
  );
  return 1;
}

function readFlag(argv, name) {
  const i = argv.indexOf(name);
  return i !== -1 && argv[i + 1] ? argv[i + 1] : undefined;
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  process.exit(main(process.argv.slice(2)));
}
