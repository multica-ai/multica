import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { checkAnalyticsDeployment, checkSources, hasRetiredBrand, sourceViolations } from "./check-branding.mjs";
import policy from "./branding-policy.json" with { type: "json" };

test("rejects old copy, including CJK suffixes and template/JSX text", () => {
  for (const text of ["Start with Mika", "Multicaへようこそ", "Mika와 시작", "登录 Multica"])
    assert.equal(hasRetiredBrand(text), true, text);
  assert.equal(sourceViolations("card.tsx", 'const card = <div>Start with Mika</div>').length, 1);
  assert.equal(sourceViolations("card.ts", 'const title = `Mika says ${name}`').length, 1);
});

function fixture(t, files) {
  const temporary = mkdtempSync(join(tmpdir(), "branding-"));
  t.after(() => rmSync(temporary, { recursive: true, force: true }));
  // An ancestor named "test" must not exempt the entire checkout.
  const root = join(temporary, "test", "repo");
  for (const directory of ["packages/core", "packages/ui", "packages/views", "apps/web", "apps/desktop", "apps/mobile"]) {
    mkdirSync(join(root, directory), { recursive: true });
  }
  for (const [name, content] of Object.entries(files)) {
    const path = join(root, name);
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, content);
  }
  return root;
}

test("walks every product source root and checks JSX, templates, metadata, HTML and brand JSON", (t) => {
  const files = {
    "packages/core/auth/error.ts": 'export const error = "Sign in to Multica";',
    "packages/ui/card.tsx": 'export const card = <div>Meet Mika</div>;',
    "packages/views/layout/title.ts": 'export const title = `Mika says ${name}`;',
    "apps/web/app/not-found.tsx": 'export default () => <span>Start with Mika</span>;',
    "apps/web/public/prompt.js": 'const title = "Welcome to Multica";',
    "apps/desktop/scripts/prompt.mjs": 'console.log("Meet Mika");',
    "apps/desktop/package.json": JSON.stringify({ productName: "Multica" }),
    "apps/desktop/src/renderer/index.html": "<title>Multica</title>",
    "apps/mobile/app/index.tsx": 'export default () => <Text>Welcome to Multica</Text>;',
    "apps/web/public/copy.json": JSON.stringify({ title: "Meet Mika" }),
    "packages/views/locales/brand/en.json": JSON.stringify({ onboarding: { title: "Start with Mika" } }),
    "apps/mobile/locales/brand/zh-Hans.json": JSON.stringify({ auth: { title: "登录 Multica" } }),
  };
  const violations = checkSources(fixture(t, files));
  assert.deepEqual(violations.map((violation) => violation.split(":")[0]).sort(), Object.keys(files).sort());
});

test("skips declared source exceptions, upstream locales, attribution and non-production files", (t) => {
  const files = Object.fromEntries(policy.sourceExceptions.map(({ path }) => [path, 'export const title = "Multica";']));
  for (const locale of ["en", "zh-Hans", "zh-Hant", "ja", "ko", "fr"]) {
    files[`packages/views/locales/${locale}/onboarding.json`] = '{"title":"Start with Mika"}';
  }
  for (const locale of ["en", "zh-Hans"]) {
    files[`apps/mobile/locales/${locale}/auth.json`] = '{"title":"Sign in to Multica"}';
  }
  for (const directory of ["node_modules", "dist", "out", ".next", ".source", ".turbo", "ios", "android", "test", "e2e"]) {
    files[`apps/web/${directory}/fixture.tsx`] = '<span>Start with Mika</span>';
  }
  for (const suffix of ["test.tsx", "spec.ts", "d.ts"]) {
    files[`packages/views/card.${suffix}`] = 'const title = "Multica";';
  }
  files["apps/desktop/package.json"] = JSON.stringify({
    productName: "Labrastro", author: { name: "Multica" }, contributors: ["Multica"],
  });
  files["apps/desktop/src/main/name.ts"] = 'const name = "Multica Canary";';
  files["apps/web/README.md"] = "Multica";
  assert.deepEqual(checkSources(fixture(t, files)), []);
  assert.equal(hasRetiredBrand("Multica Canary dev profile for Multica"), true);
});

test("CLI returns failure for a source regression and success once corrected", (t) => {
  const name = "apps/web/app/not-found.tsx";
  const root = fixture(t, { [name]: '<span>Start with Mika</span>' });
  const script = fileURLToPath(new URL("./check-branding.mjs", import.meta.url));
  const run = () => spawnSync(process.execPath, [script, root], { encoding: "utf8", timeout: 30_000 });
  const failed = run();
  assert.equal(failed.error, undefined);
  assert.equal(failed.status, 1, failed.stdout + failed.stderr);
  assert.ok(failed.stderr.includes(`${name}:1: Start with Mika`), failed.stderr);
  writeFileSync(join(root, name), '<span>Start with Mizuki</span>');
  const passed = run();
  assert.equal(passed.error, undefined);
  assert.equal(passed.status, 0, passed.stderr);
  assert.match(passed.stdout, /Brand source guard passed/);
});

test("preserves technical identities without exempting surrounding copy", () => {
  assert.equal(hasRetiredBrand("multica setup MULTICA_APP_URL @multica/core multica:// https://github.com/multica-ai/multica"), false);
  assert.equal(sourceViolations("api.ts", '// Mika\nconst MikaDefaultName = "Mizuki";').length, 0);
  assert.equal(hasRetiredBrand("X-Multica-Signature"), false);
  assert.equal(hasRetiredBrand("Use X-Multica-Signature to connect to Multica"), true);
  for (const group of Object.values(policy)) {
    for (const entry of group) assert.ok(entry.reason.trim(), JSON.stringify(entry));
  }
});


test("deployment analytics is forbidden even through empty compose or Helm passthrough", (t) => {
  for (const [path, value] of [
    ["docker-compose.selfhost.yml", "GOOGLE_ANALYTICS_ID: ${GOOGLE_ANALYTICS_ID:-}"],
    ["deploy/helm/multica/values.yaml", "googleAnalyticsId: \"\""],
    [".env.example", "GOOGLE_ANALYTICS_ID="],
    [".github/workflows/deploy.yml", "GOOGLE_ANALYTICS_ID: G-123"],
  ]) {
    assert.equal(checkAnalyticsDeployment(fixture(t, { [path]: value })).length, 1, path);
  }
  assert.deepEqual(checkAnalyticsDeployment(fixture(t, { "docker-compose.selfhost.yml": "ANALYTICS_DISABLED: true" })), []);
});
