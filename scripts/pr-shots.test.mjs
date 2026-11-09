import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { copyFileSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, renameSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { ghRunner, installPinnedGh, pinnedGh, runPrShots } from "./pr-shots";

const prUrl = "https://github.com/multica-ai/multica/pull/123";

function setup(t, body = "## Problem\n\nExisting prose and formatting.\n") {
  const directory = mkdtempSync(join(tmpdir(), "pr-shots-test-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const state = {
    body, edits: 0, uploads: 0, version: "2.103.2", failAt: 0, failAfterSave: false, beforeRead: null,
    env: {}, installs: 0, pinnedVersion: "2.103.3", install: () => "/cache/gh/bin/gh", editors: [],
  };
  const file = (name, content = name) => {
    const path = join(directory, name);
    writeFileSync(path, content);
    return path;
  };
  const fakeGh = (label, version) => (args) => {
    const ok = (stdout = "") => ({ status: 0, stdout, stderr: "" });
    if (args[0] === "--version") {
      return version() ? ok(`gh version ${version()} (test)`) : { status: 1, stdout: "", stderr: "spawnSync gh ENOENT" };
    }
    if (args[1] === "view") {
      state.beforeRead?.();
      return ok(JSON.stringify({ body: state.body, url: prUrl }));
    }
    assert.equal(args[1], "edit");
    assert.equal(args[2], prUrl, "writes must use the resolved repository/PR");
    state.edits++;
    state.editors.push(label);
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
    const status = runPrShots(args, {
      env: state.env,
      gh: fakeGh("installed", () => state.version),
      installGh: (options) => { state.installs++; return state.install(options); },
      runner: (binary) => fakeGh(binary, () => state.pinnedVersion),
      out: (s) => { stdout += s; },
      log: (s) => { stderr += `${s}\n`; },
    });
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
  state.install = () => { throw new Error("Could not set up gh 2.103.3: download failed: offline\n  curl -fL -o ..."); };
  const failed = run("123", image);
  assert.equal(failed.status, 1);
  assert.match(failed.stderr, /gh 2\.99\.0 is older than 2\.103\.2/);
  assert.match(failed.stderr, /download failed: offline\n {2}curl -fL/);
  assert.equal(state.installs, 1, "invalid inputs fail before any download");
  assert.equal(state.edits, 0);
});

test("uses the installed gh when it is at least 2.103.2", (t) => {
  const { state, file, run } = setup(t);
  for (const version of ["2.103.2", "2.104.0", "3.0.0"]) {
    state.version = version;
    assert.equal(run("123", file(`${version}.png`)).status, 0);
  }
  assert.equal(state.installs, 0);
  assert.deepEqual(state.editors, ["installed", "installed", "installed"]);
});

test("uploads with the pinned gh when the installed one is too old or missing", (t) => {
  const { state, file, run } = setup(t);
  for (const [version, reason] of [["2.86.0", /gh 2\.86\.0 is older than 2\.103\.2/], ["2.103.1", /gh 2\.103\.1 is older/], ["", /gh was not found/]]) {
    state.version = version;
    const result = run("--repo", "multica-ai/multica", "123", file(`${version || "missing"}.png`));
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stderr, reason);
    assert.match(result.stderr, /Using \/cache\/gh/);
    assert.match(result.stdout, /user-attachments\/assets\//);
  }
  assert.equal(state.installs, 3);
  assert.deepEqual(state.editors, ["/cache/gh/bin/gh", "/cache/gh/bin/gh", "/cache/gh/bin/gh"]);
});

test("a pinned gh that does not run stops before any write", (t) => {
  const { state, file, run } = setup(t);
  state.version = "2.86.0";
  state.pinnedVersion = "";
  const result = run("123", file("screen.png"));
  assert.equal(result.status, 1);
  assert.match(result.stderr, /\/cache\/gh\/bin\/gh did not run as gh 2\.103\.2\+; delete \/cache\/gh and retry/);
  assert.equal(state.edits, 0);
});

test("PR_SHOTS_GH selects the binary and is never silently replaced", (t) => {
  const { state, file, run } = setup(t);
  state.env = { PR_SHOTS_GH: "/opt/gh" };
  assert.equal(run("123", file("new.png")).status, 0);
  state.version = "2.86.0";
  const result = run("123", file("old.png"));
  assert.equal(result.status, 1);
  assert.match(result.stderr, /PR_SHOTS_GH=\/opt\/gh: gh 2\.86\.0 is older than 2\.103\.2/);
  assert.equal(state.installs, 0);
  assert.equal(state.edits, 1);
});

test("pinned release covers macOS, Linux, and Windows on x64 and arm64", () => {
  for (const os of ["macOS", "linux", "windows"]) {
    for (const cpu of ["amd64", "arm64"]) {
      const name = `gh_${pinnedGh.version}_${os}_${cpu}${os === "linux" ? ".tar.gz" : ".zip"}`;
      assert.match(pinnedGh.sha256[name] ?? "", /^[0-9a-f]{64}$/, name);
    }
  }
});

function fakeRelease(t) {
  const root = mkdtempSync(join(tmpdir(), "pr-shots-gh-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const name = "gh_9.9.9_linux_amd64";
  mkdirSync(join(root, "src", name, "bin"), { recursive: true });
  writeFileSync(join(root, "src", name, "bin", "gh"), "#!/bin/sh\necho 'gh version 9.9.9 (test)'\n", { mode: 0o644 });
  const archive = join(root, `${name}.tar.gz`);
  assert.equal(spawnSync("tar", ["-czf", archive, "-C", join(root, "src"), name]).status, 0);
  const sha256 = createHash("sha256").update(readFileSync(archive)).digest("hex");
  const fetches = [];
  const options = {
    release: { version: "9.9.9", sha256: { [`${name}.tar.gz`]: sha256 } },
    cacheRoot: join(root, "cache"),
    platform: "linux",
    arch: "x64",
    fetch: (url, destination) => { fetches.push(url); copyFileSync(archive, destination); },
  };
  return { options, fetches, cacheRoot: options.cacheRoot };
}

test("installPinnedGh verifies, extracts, and reuses the cached binary", { skip: process.platform === "win32" }, (t) => {
  const { options, fetches } = fakeRelease(t);
  const binary = installPinnedGh(options);
  assert.deepEqual(fetches, ["https://github.com/cli/cli/releases/download/v9.9.9/gh_9.9.9_linux_amd64.tar.gz"]);
  assert.ok(binary.endsWith(join("9.9.9", "linux-x64", "gh_9.9.9_linux_amd64", "bin", "gh")));
  assert.ok(statSync(binary).mode & 0o100, "binary must be executable");
  assert.match(ghRunner(binary)(["--version"]).stdout, /^gh version 9\.9\.9/);
  assert.equal(installPinnedGh(options), binary);
  assert.equal(fetches.length, 1, "a cached binary is not downloaded again");
});

test("installPinnedGh rejects a checksum mismatch and explains the manual setup", (t) => {
  const { options, cacheRoot } = fakeRelease(t);
  options.fetch = (_url, destination) => writeFileSync(destination, "tampered");
  assert.throws(() => installPinnedGh(options), (error) => {
    assert.match(error.message, /^Could not set up gh 9\.9\.9: gh_9\.9\.9_linux_amd64\.tar\.gz has SHA-256 [0-9a-f]{64}, expected/);
    assert.match(error.message, /curl -fL -o '.+gh_9\.9\.9_linux_amd64\.tar\.gz' https:\/\/github\.com\/cli\/cli\/releases\/download\/v9\.9\.9\//);
    assert.match(error.message, /\| sha256sum -c\n/);
    assert.match(error.message, /PR_SHOTS_GH/);
    return true;
  });
  assert.deepEqual(readdirSync(join(cacheRoot, "9.9.9")), [], "failed installs leave no cache entry");
  options.platform = "freebsd";
  assert.throws(() => installPinnedGh(options), /no pinned build for freebsd\/x64\nInstall any gh 2\.103\.2\+ binary/);
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
