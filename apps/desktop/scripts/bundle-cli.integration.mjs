import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { test } from "node:test";

const source = new URL("./bundle-cli.mjs", import.meta.url);
const goCaches = JSON.parse(execFileSync("go", ["env", "-json", "GOCACHE", "GOMODCACHE"], {
  encoding: "utf8", windowsHide: true, timeout: 15_000,
}));

function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), "multica-bundle-version-"));
  t.after(() => {
    assert.equal(resolve(directory), directory);
    assert.ok(directory.startsWith(join(tmpdir(), "multica-bundle-version-")));
    rmSync(directory, { recursive: true, force: true });
  });
  const scripts = join(directory, "apps/desktop/scripts");
  const server = join(directory, "server");
  mkdirSync(scripts, { recursive: true });
  mkdirSync(join(server, "cmd/multica"), { recursive: true });
  mkdirSync(join(directory, "empty-hooks"));
  copyFileSync(source, join(scripts, "bundle-cli.mjs"));
  writeFileSync(join(server, "go.mod"), "module version-fixture\n\ngo 1.26.0\n");
  // Compile only this tiny local program: no real agent, account or network.
  writeFileSync(join(server, "cmd/multica/main.go"), [
    "package main",
    'import ("fmt"; "os"; "runtime")',
    'var version = "dev"',
    'var commit = "unknown"',
    'var date = "unknown"',
    'func main() { if len(os.Args) != 2 || os.Args[1] != "--version" { os.Exit(2) };',
    'fmt.Printf("multica %s (commit: %s, built: %s)\\ngo: %s, os/arch: %s/%s\\n", version, commit, date, runtime.Version(), runtime.GOOS, runtime.GOARCH) }',
    "",
  ].join("\n"));
  const env = {
    ...process.env, ...goCaches, HOME: directory, USERPROFILE: directory,
    GOWORK: "off", GOTOOLCHAIN: "local", GOPROXY: "off", GOENV: "off",
    GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: join(directory, "empty-gitconfig"),
  };
  delete env.MULTICA_CLI_VERSION;
  const git = (...args) => execFileSync("git", args, {
    cwd: directory, env, encoding: "utf8", windowsHide: true, timeout: 15_000,
  }).trim();
  git("init", "--quiet");
  git("-c", "user.name=Version fixture", "-c", "user.email=fixture@example.invalid",
    "-c", "commit.gpgsign=false", "-c", "core.hooksPath=" + join(directory, "empty-hooks"),
    "commit", "--quiet", "--allow-empty", "-m", "fixture");
  const build = (version, { withoutGo = false } = {}) => {
    const buildEnv = { ...env };
    if (version !== undefined) buildEnv.MULTICA_CLI_VERSION = version;
    if (withoutGo) {
      for (const key of Object.keys(buildEnv)) if (key.toUpperCase() === "PATH") delete buildEnv[key];
      buildEnv.PATH = "";
    }
    return spawnSync(process.execPath, [join(scripts, "bundle-cli.mjs")], {
      cwd: directory, env: buildEnv, encoding: "utf8", windowsHide: true, timeout: 120_000,
    });
  };
  const output = () => execFileSync(join(directory, "apps/desktop/resources/bin",
    process.platform === "win32" ? "multica.exe" : "multica"), ["--version"], {
    cwd: directory, env, encoding: "utf8", windowsHide: true, timeout: 15_000,
  });
  return { git, build, output };
}

test("tagless custom checkout links the explicit release version into the actual CLI", (t) => {
  const repo = fixture(t);
  assert.equal(repo.git("tag", "--list"), "");
  const version = "0.4.36-custom.123";
  const result = repo.build(version);
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
  const output = repo.output();
  assert.equal(output.split(/\s+/)[1], version);
  assert.ok(output.startsWith("multica " + version + " (commit: " + repo.git("rev-parse", "--short", "HEAD") + ","));
});

test("invalid explicit versions fail instead of silently reverting to git describe", (t) => {
  const repo = fixture(t);
  repo.git("tag", "v0.4.36");
  for (const version of ["", "3ed46eeed", "00.4.36", "0.4.36-custom.0", "0.4.36-custom.01", "0.4.36\n", "0.4.36 -X main.commit=wrong"]) {
    const result = repo.build(version);
    assert.ifError(result.error);
    assert.notEqual(result.status, 0, "Accepted invalid MULTICA_CLI_VERSION: " + JSON.stringify(version));
    assert.match(result.stderr, /MULTICA_CLI_VERSION/);
  }
});

test("an explicit release version requires Go instead of reusing a stale binary", (t) => {
  const repo = fixture(t);
  const result = repo.build("0.4.36-custom.123", { withoutGo: true });
  assert.ifError(result.error);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /MULTICA_CLI_VERSION requires Go/);
});

test("normal tagged builds preserve their git-derived CLI version", (t) => {
  const repo = fixture(t);
  repo.git("tag", "v0.4.36");
  const result = repo.build();
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
  assert.ok(repo.output().startsWith("multica v0.4.36 (commit: "));
});
