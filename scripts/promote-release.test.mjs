import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { parse, stringify } from "yaml";
import { channels, isNewer, promoteRelease, validateRelease } from "./promote-release.mjs";

function fixture(tag = "v0.10.0") {
  const version = tag.slice(1);
  const assets = [];
  const metadata = new Map();
  const add = (name) => { const asset = { id: assets.length + 1, name, size: 123, state: "uploaded" }; assets.push(asset); return asset; };
  for (const [name, platform, arch, ext] of channels) {
    add(name);
    const cpu = platform === "linux" && arch === "x64" ? "x86_64" : arch;
    const file = add(`multica-desktop-${version}-${platform}-${cpu}.${ext}`);
    const files = [{ url: file.name, size: file.size, sha512: "hash" }];
    if (platform === "linux") {
      const rpm = add(`multica-desktop-${version}-linux-${arch === "x64" ? "x86_64" : "aarch64"}.rpm`);
      files.push({ url: rpm.name, size: rpm.size, sha512: "hash" });
    }
    metadata.set(name, stringify({ version, files }));
  }
  for (const os of ["darwin", "linux", "windows"]) {
    for (const arch of ["amd64", "arm64"]) {
      const ext = os === "windows" ? "zip" : "tar.gz";
      add(`multica_${os}_${arch}.${ext}`);
      add(`multica-cli-${version}-${os}-${arch}.${ext}`);
    }
  }
  add("checksums.txt");
  const calls = [];
  const release = { id: 42, tag_name: tag, draft: false, prerelease: false };
  const options = { tag, getRelease: async () => release, getAssets: async () => assets,
    readMetadata: async (asset) => metadata.get(asset.name), getLatest: async () => "v0.9.0",
    promote: async (tag) => { calls.push(tag); } };
  return { tag, assets, metadata, release, calls, options };
}

test("every missing upload keeps Latest unchanged and can be retried", async () => {
  for (let i = 0; i < fixture().assets.length; i++) {
    const f = fixture();
    const [missing] = f.assets.splice(i, 1);
    assert.equal((await promoteRelease(f.options)).status, "waiting", missing.name);
    assert.deepEqual(f.calls, []);
    f.assets.push(missing);
    assert.equal((await promoteRelease(f.options)).status, "promoted");
    assert.deepEqual(f.calls, [f.tag]);
  }
});

test("upload completion works in either platform order", async () => {
  for (const reverse of [false, true]) {
    const f = fixture();
    const completed = [...f.assets];
    if (reverse) completed.reverse();
    f.assets.length = 0;
    for (const asset of completed) {
      assert.equal((await promoteRelease(f.options)).status, "waiting");
      f.assets.push(asset);
    }
    assert.equal((await promoteRelease(f.options)).status, "promoted");
  }
});

test("bad metadata and incomplete assets cannot promote", async () => {
  for (const mutate of [
    (m) => { m.version = "0.9.0"; },
    (m) => { m.files = []; },
    (m) => { m.files[0].url = "missing.zip"; },
    (m) => { m.files[0].url = "https://example.com/other.zip"; },
    (m) => { m.files[0].url = "multica-desktop-0.10.0-mac-x64.zip"; },
    (m) => { m.files[0].size++; },
    (m) => { delete m.files[0].size; },
  ]) {
    const f = fixture();
    const metadata = parse(f.metadata.get("latest-mac.yml"));
    mutate(metadata);
    f.metadata.set("latest-mac.yml", stringify(metadata));
    assert.equal((await promoteRelease(f.options)).status, "waiting");
    assert.deepEqual(f.calls, []);
  }
  const f = fixture();
  f.metadata.set("latest-mac.yml", "files: [\n");
  assert.equal((await promoteRelease(f.options)).status, "waiting");
  f.assets[0].state = "starter";
  assert.equal((await promoteRelease(f.options)).status, "waiting");
});

test("semantic version ordering prevents a late older release and repeated promotion", async () => {
  assert.equal(isNewer("v0.10.0", "v0.9.9"), true);
  assert.equal(isNewer("v1.0.0", "v0.99.99"), true);
  assert.equal(isNewer("v0.10.0", null), true);
  for (const latest of ["v0.10.0", "v0.10.1", "v1.0.0"]) {
    const f = fixture();
    f.options.getLatest = async () => latest;
    assert.equal((await promoteRelease(f.options)).status, "skipped");
    assert.deepEqual(f.calls, []);
  }
  for (const tag of ["v01.2.3", "v1.2.3-rc.1", "v1.2.3;echo nope", "1.2.3"]) {
    assert.throws(() => isNewer(tag, null), /stable/);
  }
});

test("API failures and unpublished releases fail closed", async () => {
  for (const key of ["getRelease", "getAssets", "readMetadata", "getLatest", "promote"]) {
    const f = fixture();
    f.options[key] = async () => { throw new Error("API unavailable"); };
    await assert.rejects(promoteRelease(f.options), /API unavailable/);
  }
  for (const change of [{ draft: true }, { prerelease: true }, { tag_name: "v0.9.0" }]) {
    const f = fixture();
    Object.assign(f.release, change);
    await assert.rejects(validateRelease(f.tag, f.release, f.assets, f.options.readMetadata), /published stable/);
  }
});

test("promotion is serialized across tags and both upload paths use the gate", () => {
  const workflow = parse(readFileSync(new URL("../.github/workflows/promote-release.yml", import.meta.url), "utf8"));
  assert.ok(workflow.on.workflow_call);
  assert.ok(workflow.on.workflow_dispatch);
  assert.equal(workflow.permissions.contents, "write");
  const gate = workflow.jobs.promote;
  assert.equal(gate.concurrency["cancel-in-progress"], false);
  assert.doesNotMatch(gate.concurrency.group, /\$\{\{/);
  assert.ok(gate.steps.some((s) => s.uses?.startsWith("actions/checkout@") && s.with.ref === "main"));
  assert.equal(gate["continue-on-error"], undefined);
  const release = parse(readFileSync(new URL("../.github/workflows/release.yml", import.meta.url), "utf8"));
  assert.ok(release.jobs.promote.needs.includes("desktop"));
  assert.ok(release.jobs.promote.needs.includes("verify"));
  assert.equal(release.jobs.promote.uses, "./.github/workflows/promote-release.yml");
  const goreleaser = parse(readFileSync(new URL("../.goreleaser.yml", import.meta.url), "utf8"));
  assert.equal(goreleaser.release.make_latest, false);
  assert.notEqual(goreleaser.release.draft, true);
});
