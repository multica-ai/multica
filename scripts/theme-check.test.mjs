import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

const script = resolve(import.meta.dirname, "theme-check.mjs");

function theme(dir, name, { manifest = {}, css }) {
  const root = join(dir, name);
  mkdirSync(root, { recursive: true });
  writeFileSync(join(root, "manifest.json"), JSON.stringify({ apiVersion: 1, id: name, name, ...manifest }));
  writeFileSync(join(root, "theme.css"), css);
  return root;
}

function run(...args) {
  const result = spawnSync(process.execPath, [script, ...args], { encoding: "utf8" });
  return { status: result.status, stdout: result.stdout, stderr: result.stderr };
}

test("passes a good theme and fails a bad one, with every problem listed", (t) => {
  const dir = mkdtempSync(join(tmpdir(), "theme-check-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const good = theme(dir, "paper", { css: ".theme-light { --radius: 0.25rem; --page-canvas: #fffdf8; }\n" });
  const bad = theme(dir, "leaky", {
    css: [
      ".theme-light { --muted-foreground: #bbbbbb; --font-ui: Inter, sans-serif; --sidebar-width: 1px; }",
      "body { background: url(https://evil.test/?q=1); }",
    ].join("\n"),
  });

  const passing = run(good);
  assert.equal(passing.status, 0, passing.stdout + passing.stderr);
  assert.match(passing.stdout, /✓ paper \(paper\)/);

  const failing = run(good, bad, "--json");
  assert.equal(failing.status, 1);
  const [paper, leaky] = JSON.parse(failing.stdout);
  assert.equal(paper.ok, true);
  assert.equal(leaky.ok, false);
  const messages = leaky.issues.map((issue) => issue.message).join("\n");
  assert.match(messages, /theme\.css line 2: url\("https:\/\/evil\.test\/\?q=1"\)/);
  assert.match(messages, /--sidebar-width is not a public variable/);
  assert.match(messages, /--font-ui names sans-serif/);
  assert.match(messages, /--muted-foreground is below 4\.5:1 on .*--surface \(\d\.\d\d:1\)/);
});

test("explains a path that is not a theme", () => {
  const result = run(join(tmpdir(), "no-such-theme-folder"));
  assert.equal(result.status, 1);
  assert.match(result.stdout, /no-such-theme-folder/);
});
