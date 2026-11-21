import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { runPrShots } from "./pr-shots";

const prUrl = "https://github.com/multica-ai/multica/pull/123";

function setup(t, body = "## Problem\n\nExisting prose and formatting.\n") {
  const directory = mkdtempSync(join(tmpdir(), "pr-shots-test-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const state = { body, edits: 0, uploads: 0, version: "2.103.2", failAt: 0, failAfterSave: false, beforeRead: null };
  const file = (name, content = name) => {
    const path = join(directory, name);
    writeFileSync(path, content);
    return path;
  };
  const gh = (args) => {
    const ok = (stdout = "") => ({ status: 0, stdout, stderr: "" });
    if (args[0] === "--version") return ok(`gh version ${state.version} (test)`);
    if (args[1] === "view") {
      state.beforeRead?.();
      return ok(JSON.stringify({ body: state.body, url: prUrl }));
    }
    assert.equal(args[1], "edit");
    assert.equal(args[2], prUrl, "writes must use the resolved repository/PR");
    state.edits++;
    if (state.failAt === state.edits) return { status: 1, stdout: "", stderr: "upload denied" };
    let body = readFileSync(args[args.indexOf("--body-file") + 1], "utf8");
    if (args.includes("--attach")) {
      const staged = args[args.indexOf("--attach") + 1];
      assert.doesNotMatch(staged, /[#\[\]]/, "upload paths must not become gh alt text or Markdown syntax");
      assert.ok(readFileSync(staged).length);
      const url = `https://github.com/user-attachments/assets/attachment-${++state.uploads}`;
      body = body.replace(`![](${staged})`, url).split(staged).join(url);
    }
    state.body = body;
    return state.failAfterSave ? { status: 1, stdout: prUrl, stderr: "connection lost after write" } : ok(prUrl);
  };
  const run = (...args) => {
    let stdout = "", stderr = "";
    const status = runPrShots(args, { gh, out: (s) => { stdout += s; }, log: (s) => { stderr += `${s}\n`; } });
    return { status, stdout, stderr };
  };
  return { state, file, run, directory };
}

test("preserves prose, emits attachment Markdown, and reuses content after a rename", (t) => {
  const { state, file, run, directory } = setup(t);
  const original = state.body;
  const image = file("before [login] #1.png");
  const first = run("--repo", "multica-ai/multica", "123", image);
  assert.equal(first.status, 0, first.stderr);
  assert.ok(state.body.startsWith(original));
  assert.match(first.stdout, /^!\[before \\\[login\\\] #1\.png\]\(https:\/\/github\.com\/user-attachments\/assets\//);
  assert.doesNotMatch(first.stdout, /pr-shots:|pr-shots-test-/);
  const renamed = join(directory, "renamed.png");
  renameSync(image, renamed);
  const second = run("123", renamed, renamed);
  assert.equal(second.status, 0, second.stderr);
  assert.equal(second.stdout, first.stdout);
  assert.equal(state.uploads, 1);
  assert.equal(state.edits, 1);
});

test("changed content at the same path is uploaded again", (t) => {
  const { state, file, run } = setup(t);
  const image = file("screen.png", "before");
  assert.equal(run("123", image).status, 0);
  writeFileSync(image, "after");
  assert.equal(run("123", image).status, 0);
  assert.equal(state.uploads, 2);
});

test("failed batch retains successes and retry uploads only missing files", (t) => {
  const { state, file, run } = setup(t);
  const files = [file("one.png"), file("two.png"), file("three.png")];
  state.failAt = 2;
  const failed = run("123", ...files);
  assert.equal(failed.status, 1);
  assert.match(failed.stdout, /attachment-1/);
  assert.match(failed.stderr, /Failed two.png: upload denied/);
  assert.match(failed.stderr, /Not attempted: three.png/);
  assert.equal(state.uploads, 1);
  state.failAt = 0;
  const retried = run("123", ...files);
  assert.equal(retried.status, 0, retried.stderr);
  assert.equal(state.uploads, 3);
  assert.equal((state.body.match(/pr-shots:sha256=/g) ?? []).length, 3);
});

test("nonzero gh exit after a successful write is read back and not reuploaded", (t) => {
  const { state, file, run } = setup(t);
  const image = file("screen.png");
  state.failAfterSave = true;
  const first = run("123", image);
  assert.equal(first.status, 1);
  assert.match(first.stdout, /attachment-1/);
  assert.match(first.stderr, /Attachment saved, but gh exited 1/);
  state.failAfterSave = false;
  assert.equal(run("123", image).status, 0);
  assert.equal(state.uploads, 1);
});

test("migration replaces the image in place, preserving captions and alt text", (t) => {
  const old = "https://github.com/multica-ai/multica/raw/2492562/pr-10077/loader-frames.png";
  const before = `Before caption.\n\n![Original alt](${old})\n\nAfter caption.\n`;
  const { state, file, run } = setup(t, before);
  const image = file("screen.png");
  const args = ["--replace", old, "123", image];
  state.failAt = 1;
  assert.equal(run(...args).status, 1);
  assert.equal(state.body, before, "failed migration must retain the old working link");
  state.failAt = 0;
  const migrated = run(...args);
  assert.equal(migrated.status, 0, migrated.stderr);
  assert.match(migrated.stdout, /^!\[Original alt\]/);
  assert.ok(state.body.startsWith("Before caption.\n\n"));
  assert.ok(state.body.endsWith("\n\nAfter caption.\n"));
  assert.ok(!state.body.includes(old));
  assert.equal(run(...args).status, 0);
  assert.equal(state.uploads, 1);
});

test("reads the latest body between uploads", (t) => {
  const { state, file, run } = setup(t);
  const one = file("one.png"), two = file("two.png");
  state.beforeRead = () => {
    if (state.uploads === 1 && !state.body.includes("New author note.")) state.body += "\nNew author note.\n";
  };
  assert.equal(run("123", one, two).status, 0);
  assert.match(state.body, /New author note\./);
});

test("videos emit a player URL on its own line and are deduplicated", (t) => {
  const { state, file, run } = setup(t);
  const video = file("interaction.webm");
  const first = run("123", video);
  assert.equal(first.status, 0, first.stderr);
  assert.match(first.stdout, /^https:\/\/github\.com\/user-attachments\/assets\/attachment-1\n\n$/);
  assert.equal(run("123", video).stdout, first.stdout);
  assert.equal(state.uploads, 1);
});

test("preflights all files and gh version before any writes", (t) => {
  const { state, file, run, directory } = setup(t);
  const image = file("screen.png");
  assert.equal(run("123", image, join(directory, "missing.png")).status, 1);
  assert.equal(run("123", image, file("secret.txt")).status, 1);
  assert.equal(run("123", file("empty.png", "")).status, 1);
  state.version = "2.99.0";
  assert.match(run("123", image).stderr, /Upgrade GitHub CLI/);
  assert.equal(state.edits, 0);
});

test("bad migration targets and unsupported arguments do not change the PR", (t) => {
  const { state, file, run } = setup(t);
  const image = file("screen.png");
  assert.equal(run("--replace", "https://example.com/missing.png", "123", image).status, 1);
  assert.equal(run("--replace", "https://example.com/video.mp4", "123", file("video.mp4")).status, 1);
  assert.equal(run("--replace", "https://example.com/old.png", "123", image, image).status, 1);
  assert.equal(run("--unexpected", "123", image).status, 1);
  assert.equal(state.edits, 0);
});
