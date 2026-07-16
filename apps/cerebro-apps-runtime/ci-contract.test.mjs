import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import test from "node:test";

const repoRoot = resolve(import.meta.dirname, "../..");

test("CI builds the worker image before the functional container test", async () => {
  const [workflow, integrationTest] = await Promise.all([
    readFile(resolve(repoRoot, ".github/workflows/ci.yml"), "utf8"),
    readFile(resolve(import.meta.dirname, "container-integration.test.mjs"), "utf8"),
  ]);

  assert.match(workflow, /name: Build mini-app worker image/);
  assert.match(workflow, /CEREBRO_APPS_WORKER_IMAGE:/);
  assert.doesNotMatch(integrationTest, /\["build",/);
  assert.match(integrationTest, /process\.env\.CEREBRO_APPS_WORKER_IMAGE/);
});
