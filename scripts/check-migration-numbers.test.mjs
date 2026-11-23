import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { findMigrationCollisions, summarize } from "./check-migration-numbers.mjs";

const script = fileURLToPath(new URL("./check-migration-numbers.mjs", import.meta.url));

test("a number main took after the branch forked collides", () => {
  const { added, collisions } = findMigrationCollisions({
    mergeBase: ["718_a"],
    base: ["718_a", "719_main", "720_main", "721_main"],
    head: ["718_a", "719_branch"],
  });
  assert.deepEqual(added, ["719_branch"]);
  assert.deepEqual(collisions, [
    {
      stem: "719_branch",
      number: 719,
      conflictsWith: [{ stem: "719_main", addedByHead: false }],
      suggestedNumber: 722,
    },
  ]);
  assert.equal(summarize(collisions, "origin/main"), "Migration 719 is already taken; renumber above the highest on origin/main");
});

test("a branch that renumbered past main is clear", () => {
  const { added, collisions } = findMigrationCollisions({
    mergeBase: ["718_a"],
    base: ["718_a", "719_main"],
    head: ["718_a", "720_branch"],
  });
  assert.deepEqual(added, ["720_branch"]);
  assert.deepEqual(collisions, []);
});

test("two migrations the branch adds with one number both collide", () => {
  const { collisions } = findMigrationCollisions({
    mergeBase: ["718_a"],
    base: ["718_a"],
    head: ["718_a", "719_x", "719_y"],
  });
  assert.deepEqual(
    collisions.map(({ stem, suggestedNumber }) => [stem, suggestedNumber]),
    [["719_x", 720], ["719_y", 721]],
  );
  assert.equal(collisions[0].conflictsWith[0].addedByHead, true);
  assert.equal(summarize(collisions, "main"), "Migration 719 is already taken; renumber above the highest on main");
});

test("renaming a migration within its own number is not a collision", () => {
  const { collisions } = findMigrationCollisions({
    mergeBase: ["718_a", "719_old_name"],
    base: ["718_a", "719_old_name"],
    head: ["718_a", "719_new_name"],
  });
  assert.deepEqual(collisions, []);
});

test("historical duplicate numbers below 129 are ignored", () => {
  const { collisions } = findMigrationCollisions({
    mergeBase: [],
    base: ["100_main"],
    head: ["100_branch"],
  });
  assert.deepEqual(collisions, []);
});

test("the CLI catches a collision the branch's own base never had", (t) => {
  const dir = mkdtempSync(join(tmpdir(), "migration-numbers-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const env = {
    ...process.env,
    GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null",
    GIT_AUTHOR_NAME: "CI test", GIT_AUTHOR_EMAIL: "ci@example.invalid",
    GIT_COMMITTER_NAME: "CI test", GIT_COMMITTER_EMAIL: "ci@example.invalid",
  };
  delete env.GITHUB_ACTIONS;
  const git = (...args) => execFileSync("git", args, { cwd: dir, env, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  const migrations = join(dir, "server", "migrations");
  const addMigration = (stem) => {
    writeFileSync(join(migrations, `${stem}.up.sql`), `-- ${stem}\n`);
    writeFileSync(join(migrations, `${stem}.down.sql`), `-- ${stem}\n`);
  };
  const run = (...args) => spawnSync("node", [script, ...args], { cwd: dir, env, encoding: "utf8" });

  mkdirSync(migrations, { recursive: true });
  git("init", "-b", "main");
  addMigration("718_existing");
  git("add", ".");
  git("commit", "-m", "base");

  git("checkout", "-b", "feature");
  addMigration("719_feature");
  git("add", ".");
  git("commit", "-m", "feature migration");
  assert.equal(run("--base", "main").status, 0, "clear while main has not moved");

  git("checkout", "main");
  addMigration("719_other");
  addMigration("720_other");
  git("add", ".");
  git("commit", "-m", "another PR lands first");
  assert.equal(spawnSync("git", ["merge-tree", "--write-tree", "main", "feature"], { cwd: dir, env }).status, 0,
    "no textual conflict, so nothing else stops the merge");

  git("checkout", "feature");
  const stale = run("--base", "main");
  assert.equal(stale.status, 1);
  assert.match(stale.stdout, /719_feature uses migration number 719, already taken by 719_other; renumber it to 721/);

  for (const direction of ["up", "down"]) {
    renameSync(join(migrations, `719_feature.${direction}.sql`), join(migrations, `721_feature.${direction}.sql`));
  }
  git("add", "-A");
  git("commit", "-m", "renumber");
  const fixed = run("--base", "main", "--json");
  assert.equal(fixed.status, 0);
  assert.deepEqual(JSON.parse(fixed.stdout).added, ["721_feature"]);

  assert.equal(run("--base", "missing-ref").status, 2);
});
