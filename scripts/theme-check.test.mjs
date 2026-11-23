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

test("fails a theme on an option value other than the default", (t) => {
  const dir = mkdtempSync(join(tmpdir(), "theme-check-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const screens = theme(dir, "screens", {
    manifest: {
      options: [
        { id: "screen", type: "select", label: "Screen", default: "prompt", choices: [
          { value: "prompt", label: "Prompt" },
          { value: "amber", label: "Amber" },
        ] },
      ],
    },
    css: ':root[data-theme-option-screen="amber"].theme-dark { --muted-foreground: #5a3e00; }\n',
  });

  const text = run(screens);
  assert.equal(text.status, 1, text.stdout + text.stderr);
  assert.match(text.stdout, /Contrast, dark mode \(Screen = Amber\): --muted-foreground is below 4\.5:1/);
  assert.match(text.stdout, /pairs checked at the default options and 1 other option value, \d+ below WCAG AA\. Lowest: --muted-foreground on .*, dark \(Screen = Amber\)/);

  const [report] = JSON.parse(run(screens, "--json").stdout);
  const failing = report.contrast.filter((c) => c.ratio < c.required);
  assert.ok(failing.length > 0);
  assert.ok(failing.every((c) => c.variant?.option === "screen" && c.variant.value === "amber"));
});

test("writes a --json report larger than a pipe buffer in full", (t) => {
  const dir = mkdtempSync(join(tmpdir(), "theme-check-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const choices = Array.from({ length: 8 }, (_, i) => ({ value: `s${i}`, label: `Screen ${i}` }));
  const screens = theme(dir, "screens", {
    manifest: { options: [{ id: "screen", type: "select", label: "Screen", default: "s0", choices }] },
    css: ".theme-light { --radius: 0.25rem; }\n",
  });

  const result = run(screens, "--json");
  assert.equal(result.status, 0, result.stderr);
  assert.ok(result.stdout.length > 65536, `the report is only ${result.stdout.length} bytes`);
  const [report] = JSON.parse(result.stdout);
  assert.equal(report.ok, true);
});

test("checks the Night Sky example's glass over its backdrop colour, with nothing left unchecked", () => {
  const result = run(resolve(import.meta.dirname, "../examples/themes/night-sky"), "--json");
  assert.equal(result.status, 0, result.stdout + result.stderr);
  const [report] = JSON.parse(result.stdout);
  assert.equal(report.ok, true);
  assert.deepEqual(report.issues, []);
  assert.ok(report.contrast.some((c) => c.glass && c.background === "--page-canvas"));
  assert.ok(report.contrast.some((c) => c.glass && c.background === "--app-shell"));
});

test("explains a path that is not a theme", () => {
  const result = run(join(tmpdir(), "no-such-theme-folder"));
  assert.equal(result.status, 1);
  assert.match(result.stdout, /no-such-theme-folder/);
});
