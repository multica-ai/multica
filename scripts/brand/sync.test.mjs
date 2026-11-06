import assert from "node:assert/strict";
import { cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { sync, targets } from "./sync.mjs";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const SOURCE = "packages/ui/lib/multica-brand.ts";

function copyOfRepo(t) {
  const root = mkdtempSync(join(tmpdir(), "brand-sync-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "packages/ui/lib"), { recursive: true });
  writeFileSync(join(root, "packages/ui/package.json"), readFileSync(join(repo, "packages/ui/package.json")));
  return root;
}

test("the committed copies match multica-brand.ts", async () => {
  assert.deepEqual(await sync(repo, { check: true }), []);
});

test("moving a coordinate without regenerating fails the check, and regenerating fixes it", async (t) => {
  const root = copyOfRepo(t);
  for (const [rel] of await targets(repo)) {
    mkdirSync(dirname(join(root, rel)), { recursive: true });
    cpSync(join(repo, rel), join(root, rel));
  }
  const source = readFileSync(join(repo, SOURCE), "utf8");
  assert.match(source, /const gap = 12;/);
  writeFileSync(join(root, SOURCE), source.replace("const gap = 12;", "const gap = 13;"));

  const drifted = await sync(root, { check: true });
  for (const rel of ["apps/mobile/components/brand/multica-logo.tsx", "server/cmd/multica/cmd_auth.go", "docs/assets/logo-light.svg"]) {
    assert.ok(drifted.includes(rel), `${rel} should drift`);
  }
  assert.ok(!drifted.includes("apps/web/public/favicon.svg"), "the icon does not use the wordmark");

  assert.deepEqual((await sync(root)).sort(), [...drifted].sort());
  assert.deepEqual(await sync(root, { check: true }), []);
});
