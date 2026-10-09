import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import test from "node:test";

test("image checks need only a shallow head and the exact base, not full history", (t) => {
  const dir = mkdtempSync(join(tmpdir(), "image-budget-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const source = join(dir, "source");
  const checkout = join(dir, "checkout");
  mkdirSync(source);
  const env = {
    ...process.env,
    GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null",
    GIT_AUTHOR_NAME: "CI test", GIT_AUTHOR_EMAIL: "ci@example.invalid",
    GIT_COMMITTER_NAME: "CI test", GIT_COMMITTER_EMAIL: "ci@example.invalid",
  };
  const git = (cwd, ...args) => execFileSync("git", args, { cwd, env, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
  git(source, "init", "-b", "main");
  writeFileSync(join(source, "existing.png"), Buffer.alloc(400 * 1024));
  git(source, "add", ".");
  git(source, "commit", "-m", "base");
  const base = git(source, "rev-parse", "HEAD");
  // Put an intermediate commit between base and head to prove no ancestry is needed.
  writeFileSync(join(source, "README.md"), "intermediate\n");
  git(source, "add", ".");
  git(source, "commit", "-m", "intermediate");
  const intermediate = git(source, "rev-parse", "HEAD");
  writeFileSync(join(source, "new.png"), Buffer.alloc(350 * 1024));
  git(source, "add", ".");
  git(source, "commit", "-m", "head");
  git(dir, "clone", "--depth=1", pathToFileURL(source).href, checkout);
  git(checkout, "fetch", "--no-tags", "--depth=1", "origin", base);
  assert.equal(git(checkout, "rev-parse", "--is-shallow-repository"), "true");
  assert.notEqual(spawnSync("git", ["cat-file", "-e", intermediate], { cwd: checkout, env }).status, 0);

  const script = fileURLToPath(new URL("./check-image-budget.mjs", import.meta.url));
  const check = (body = "") => spawnSync(process.execPath, [script, "--base", base], {
    cwd: checkout, env: { ...env, PR_BODY: body }, encoding: "utf8",
  });
  const oversized = check();
  assert.equal(oversized.status, 1, oversized.stderr);
  assert.match(oversized.stdout, /new\.png/);
  assert.doesNotMatch(oversized.stdout, /existing\.png/);
  assert.equal(check("Oversized image exemption: required test fixture").status, 0);
  writeFileSync(join(checkout, "new.png"), Buffer.alloc(100));
  writeFileSync(join(checkout, "existing.png"), Buffer.alloc(350 * 1024));
  assert.equal(check().status, 0, "unchanged or shrinking oversized files should pass");
});
