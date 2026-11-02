import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { checkDocsDistribution, distributionViolations } from "./check-docs-distribution.mjs";

test("rejects upstream installation and container upgrade instructions", () => {
  for (const text of [
    "[Download](https://multica.ai/download)",
    "curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash",
    "irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex",
    "https://github.com/multica-ai/multica/releases/latest",
    "brew install multica-ai/tap/multica",
    "brew tap multica-ai/tap",
    "ghcr.io/multica-ai/multica:latest",
    "docker compose -f docker-compose.selfhost.yml pull",
    "`docker compose pull` + `up -d`",
    "${MULTICA_IMAGE_TAG:-latest}",
    "MULTICA_IMAGE_TAG=latest",
    "git clone --depth 1 https://github.com/multica-ai/multica.git",
  ]) assert.equal(distributionViolations("guide.zh.mdx", text).length, 1, text);
});

test("allows internal releases, local builds, and upstream source attribution", () => {
  const content = [
    "curl -fsSL https://multica.outlune.com/downloads/install.sh | bash",
    "https://multica.outlune.com/downloads/desktop/",
    "https://multica.outlune.com/downloads/cli/v<version>/",
    "https://github.com/multica-ai/multica/blob/main/AGENTS.md",
    "https://github.com/multica-ai/multica/issues/9498",
    "git clone https://github.com/AstralSolipsism/multica.git",
    "make candidate-images",
    "docker compose -f docker-compose.selfhost.yml ps",
    "docker compose -f docker-compose.selfhost.yml up -d",
    "brew install go",
  ].join("\n");
  assert.deepEqual(distributionViolations("guide.mdx", content), []);
});

test("checks localized pages and embedded UI copy, and exits nonzero on regressions", (t) => {
  const root = mkdtempSync(join(tmpdir(), "docs-distribution-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const files = {
    "apps/docs/content/docs/install.fr.mdx": "Installation\nhttps://multica.ai/download",
    "apps/docs/components/install.tsx": 'const url = "https://multica.ai/download";',
    "apps/docs/lib/locale-link.test.ts": 'const url = "https://multica.ai/download";',
    "apps/docs/.source/generated.ts": 'const url = "https://multica.ai/download";',
  };
  for (const [name, content] of Object.entries(files)) {
    mkdirSync(dirname(join(root, name)), { recursive: true });
    writeFileSync(join(root, name), content);
  }
  assert.deepEqual(checkDocsDistribution(root).sort(), [
    "apps/docs/components/install.tsx:1: upstream download page",
    "apps/docs/content/docs/install.fr.mdx:2: upstream download page",
  ]);
  const script = fileURLToPath(new URL("./check-docs-distribution.mjs", import.meta.url));
  const run = () => spawnSync(process.execPath, [script, root], { encoding: "utf8", timeout: 30_000 });
  const failed = run();
  assert.equal(failed.error, undefined);
  assert.equal(failed.status, 1, failed.stdout + failed.stderr);
  assert.match(failed.stderr, /install\.fr\.mdx:2/);
  for (const name of Object.keys(files)) writeFileSync(join(root, name), "https://multica.outlune.com/downloads/");
  const passed = run();
  assert.equal(passed.error, undefined);
  assert.equal(passed.status, 0, passed.stderr);
  assert.match(passed.stdout, /Documentation distribution guard passed/);
});
