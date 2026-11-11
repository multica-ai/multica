// The check guards a blind spot that is invisible by construction: a component
// under a wildcard export registers as a knip entry point, so knip reports it
// as used no matter what. A guard for an invisible failure is worth exactly as
// much as its proof that it still fires.
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import test from "node:test";

const script = resolve(import.meta.dirname, "check-ui-wildcard-exports.mjs");

// Keep fixture imports and wildcard exports shaped like the real workspaces.
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), "ui-exports-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const write = (rel, text) => {
    mkdirSync(dirname(join(root, rel)), { recursive: true });
    writeFileSync(join(root, rel), text);
  };
  const remove = (rel) => rmSync(join(root, rel));
  write("packages/ui/package.json", JSON.stringify({
    name: "@multica/ui",
    exports: {
      "./components/ui/*": "./components/ui/*.tsx",
      "./components/common/*": "./components/common/*.tsx",
      "./markdown/*": "./markdown/*.tsx",
      "./hooks/*": "./hooks/*.ts",
    },
  }));
  mkdirSync(join(root, "packages/views"), { recursive: true });
  const check = () => {
    const result = spawnSync(process.execPath, [script, root], { encoding: "utf8" });
    return { status: result.status, output: result.stdout + result.stderr };
  };
  const passes = (message) => {
    const { status, output } = check();
    assert.equal(status, 0, `${message}\n${output}`);
  };
  const failsNaming = (names, message) => {
    const { status, output } = check();
    assert.equal(status, 1, `${message}\n${output}`);
    for (const name of names) assert.ok(output.includes(name), `should name ${name}\n${output}`);
  };
  return { write, remove, passes, failsNaming };
}

const importer = "packages/views/zz-probe-importer.tsx";

test("an unimported component fails and is named until a consumer imports it", (t) => {
  const { write, passes, failsNaming } = fixture(t);
  passes("a clean tree should pass");
  write("packages/ui/components/ui/zz-unused-export-probe.tsx", "export function UnusedExportProbe() { return null; }\n");
  failsNaming(["zz-unused-export-probe.tsx"], "a zero-reference component should fail");
  write(importer, 'export { UnusedExportProbe } from "@multica/ui/components/ui/zz-unused-export-probe";\n');
  passes("a real consumer should make the component live");
});

test("a relative import of a same-named file outside UI cannot vouch for it", (t) => {
  const { write, failsNaming } = fixture(t);
  write("packages/views/onboarding/components/option-card.tsx", "export function OptionCard() { return null; }\n");
  write("packages/views/onboarding/steps/welcome.tsx", 'export { OptionCard } from "../components/option-card";\n');
  write("packages/ui/components/ui/option-card.tsx", "export function OptionCard() { return null; }\n");
  failsNaming(["components/ui/option-card.tsx"], "basename matching must not mark a dead component as used");
});

// Liveness is reachability from outside the guarded directories, not a
// reference count.
test("an unreachable import chain or cycle stays dead until something outside reaches it", (t) => {
  const { write, passes, failsNaming } = fixture(t);
  write("packages/ui/components/ui/zz-cycle-a.tsx", 'import { CycleB } from "./zz-cycle-b";\nexport const CycleA = () => CycleB();\n');
  write("packages/ui/components/ui/zz-cycle-b.tsx", "export const CycleB = () => null;\n");
  failsNaming(["zz-cycle-a.tsx", "zz-cycle-b.tsx"], "an A -> B chain must not vouch for itself");
  write("packages/ui/components/ui/zz-cycle-b.tsx", 'import { CycleA } from "./zz-cycle-a";\nexport const CycleB = () => CycleA;\n');
  failsNaming(["zz-cycle-a.tsx", "zz-cycle-b.tsx"], "an A <-> B cycle must not vouch for itself");
  write(importer, 'export { CycleA } from "@multica/ui/components/ui/zz-cycle-a";\n');
  passes("an external consumer should make both halves live");
});

// A component passes through this state the moment its last real importer is
// deleted, so a text scan would keep vouching for it exactly when it stops
// being used.
test("commented-out, stringified and mock-only mentions are not imports", (t) => {
  const { write, remove, failsNaming } = fixture(t);
  write("packages/ui/components/ui/zz-comment-probe.tsx", "export function CommentProbe() { return null; }\n");
  write(importer, [
    '// import { CommentProbe } from "@multica/ui/components/ui/zz-comment-probe";',
    'export const note = \'import { CommentProbe } from "@multica/ui/components/ui/zz-comment-probe"\';',
  ].join("\n"));
  failsNaming(["zz-comment-probe.tsx"], "a comment or string must not count as an edge");
  remove("packages/ui/components/ui/zz-comment-probe.tsx");
  write("packages/ui/components/ui/zz-mock-only.tsx", "export function MockOnly() { return null; }\n");
  write(importer, 'import { vi } from "vitest"; vi.mock("@multica/ui/components/ui/zz-mock-only");\n');
  failsNaming(["zz-mock-only.tsx"], "a mock-only mention must not vouch for a product");
});

// Vitest discovers *.test.{ts,tsx} without product imports. Such files in
// wildcard directories are exempt from reporting, but cannot vouch for unused
// production files. Non-contract *.spec files remain guarded.
for (const [dir, ext] of [["components/ui", "tsx"], ["components/common", "tsx"], ["markdown", "tsx"], ["hooks", "ts"]]) {
  test(`colocated tests in ${dir} are exempt but cannot vouch for a product`, (t) => {
    const { write, remove, passes, failsNaming } = fixture(t);
    const testFile = `packages/ui/${dir}/zz-unused-runtime.test.${ext}`;
    write(testFile, 'import { it } from "vitest";\nit("discovered", () => {});\n');
    passes(`a Vitest-discovered test in ${dir} should not need a product import`);
    write(`packages/ui/${dir}/zz-unused-runtime.${ext}`, "export function RuntimeProbe() { return null; }\n");
    write(testFile, 'import { RuntimeProbe } from "./zz-unused-runtime";\nimport { it } from "vitest";\nit("probe", () => RuntimeProbe());\n');
    failsNaming([`${dir}/zz-unused-runtime.${ext}`], `a colocated test in ${dir} must not vouch for an unused product`);
    write(importer, `export { RuntimeProbe } from "@multica/ui/${dir}/zz-unused-runtime";\n`);
    passes(`a real consumer should make the product in ${dir} live`);
    for (const rel of [importer, testFile, `packages/ui/${dir}/zz-unused-runtime.${ext}`]) remove(rel);
    write(`packages/ui/${dir}/zz-not-discovered.spec.${ext}`, "export function StillGuarded() { return null; }\n");
    failsNaming([`${dir}/zz-not-discovered.spec.${ext}`], `a spec file in ${dir} must remain guarded`);
  });
}
