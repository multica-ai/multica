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

for (const { name, from, to, drift, stay } of [
  {
    name: "a wordmark coordinate",
    from: "const gap = 12;",
    to: "const gap = 13;",
    drift: ["apps/mobile/components/brand/multica-logo.tsx", "server/cmd/multica/cmd_auth.go", "docs/assets/logo-light.svg"],
    stay: ["apps/web/public/favicon.svg"],
  },
  {
    name: "the tile radius",
    from: "export const ICON_TILE_RADIUS = 22.5;",
    to: "export const ICON_TILE_RADIUS = 30;",
    drift: ["apps/web/public/favicon.svg", "apps/docs/app/icon.svg"],
    stay: ["apps/web/public/icons/icon.svg", "server/cmd/multica/cmd_auth.go"],
  },
]) {
  test(`changing ${name} without regenerating fails the check, and regenerating fixes it`, async (t) => {
    const root = copyOfRepo(t);
    for (const [rel] of await targets(repo)) {
      mkdirSync(dirname(join(root, rel)), { recursive: true });
      cpSync(join(repo, rel), join(root, rel));
    }
    const source = readFileSync(join(repo, SOURCE), "utf8");
    assert.ok(source.includes(from), `${SOURCE} should contain ${from}`);
    writeFileSync(join(root, SOURCE), source.replace(from, to));

    const drifted = await sync(root, { check: true });
    for (const rel of drift) assert.ok(drifted.includes(rel), `${rel} should drift`);
    for (const rel of stay) assert.ok(!drifted.includes(rel), `${rel} should not drift`);

    assert.deepEqual((await sync(root)).sort(), [...drifted].sort());
    assert.deepEqual(await sync(root, { check: true }), []);
  });
}
