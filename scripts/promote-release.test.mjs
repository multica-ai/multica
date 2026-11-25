import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import test from "node:test";
import { parse, stringify } from "yaml";
import { channels, isNewer, promoteRelease, validateRelease } from "./promote-release.mjs";

function fixture(tag = "v0.10.0") {
  const version = tag.slice(1);
  const assets = [];
  const metadata = new Map();
  const add = (name, contents = `release fixture for ${name}\n`) => {
    const asset = {
      id: assets.length + 1,
      name,
      size: Buffer.byteLength(contents),
      state: "uploaded",
      digest: `sha256:${createHash("sha256").update(contents).digest("hex")}`,
    };
    assets.push(asset);
    return asset;
  };
  const metadataFile = (asset) => ({ url: asset.name, size: asset.size, sha512: "hash" });
  for (const [name, platform, arch, ext] of channels) {
    add(name);
    const cpu = platform === "linux" && arch === "x64" ? "x86_64" : arch;
    const file = add(`multica-desktop-${version}-${platform}-${cpu}.${ext}`);
    const files = [metadataFile(file)];
    if (platform === "linux") {
      const deb = add(`multica-desktop-${version}-linux-${arch === "x64" ? "amd64" : "arm64"}.deb`);
      const rpm = add(`multica-desktop-${version}-linux-${arch === "x64" ? "x86_64" : "aarch64"}.rpm`);
      files.push(metadataFile(deb), metadataFile(rpm));
    } else if (platform === "mac") {
      const dmg = add(`multica-desktop-${version}-mac-${arch}.dmg`);
      files.push(metadataFile(dmg));
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
  const calls = [];
  const manifests = [];
  const release = { id: 42, tag_name: tag, draft: false, prerelease: false };
  const options = { tag, getRelease: async () => release, getAssets: async () => assets,
    readMetadata: async (asset) => metadata.get(asset.name), getLatest: async () => "v0.9.0",
    uploadChecksums: async (tag, checksums) => {
      assert.equal(tag, release.tag_name);
      manifests.push(checksums);
      const index = assets.findIndex((asset) => asset.name === "checksums.txt");
      if (index >= 0) assets.splice(index, 1);
      add("checksums.txt", checksums);
    },
    promote: async (tag) => {
      assert.ok(assets.some((asset) => asset.name === "checksums.txt"));
      calls.push(tag);
    } };
  return { tag, assets, metadata, release, calls, manifests, options, add };
}

test("every missing upload keeps Latest unchanged and can be retried", async () => {
  for (let i = 0; i < fixture().assets.length; i++) {
    const f = fixture();
    const [missing] = f.assets.splice(i, 1);
    assert.equal((await promoteRelease(f.options)).status, "waiting", missing.name);
    assert.deepEqual(f.manifests, []);
    assert.deepEqual(f.calls, []);
    f.assets.push(missing);
    assert.equal((await promoteRelease(f.options)).status, "promoted");
    assert.deepEqual(f.calls, [f.tag]);
  }
});

test("either upload order creates one checksum per CLI archive and Desktop asset", async () => {
  for (const macFirst of [false, true]) {
    const f = fixture();
    f.add("multica-desktop-0.10.0-windows-arm64.exe.blockmap");
    f.add("multica-desktop-0.10.0-mac-x64.dmg.blockmap");
    const expected = f.assets.filter(({ name }) => name.startsWith("multica"))
      .map(({ name, digest }) => `${digest.slice("sha256:".length)}  ${name}`);
    const macAssets = f.assets.filter(({ name }) => /-mac-|-mac\.yml$/.test(name));
    const ciAssets = f.assets.filter((asset) => !macAssets.includes(asset));
    const [first, second] = macFirst ? [macAssets, ciAssets] : [ciAssets, macAssets];
    f.assets.length = 0;
    const cliChecksums = expected.filter((line) => /  multica(?:_|-cli-)/.test(line)).join("\n") + "\n";
    if (!macFirst) f.add("checksums.txt", cliChecksums);
    f.assets.push(...first);
    assert.equal((await promoteRelease(f.options)).status, "waiting");
    assert.deepEqual(f.manifests, []);
    if (macFirst) f.add("checksums.txt", cliChecksums);
    f.assets.push(...second);
    assert.equal((await promoteRelease(f.options)).status, "promoted");
    assert.equal(f.manifests.length, 1);
    const lines = f.manifests[0].trimEnd().split("\n");
    assert.deepEqual(lines.sort(), expected.sort());
    assert.deepEqual(f.calls, [f.tag]);
  }
});

test("repeated promotion skips identical checksums and refreshes replaced assets", async () => {
  const f = fixture();
  assert.equal((await promoteRelease(f.options)).status, "promoted");
  f.options.getLatest = async () => f.tag;
  assert.equal((await promoteRelease(f.options)).status, "skipped");
  assert.equal(f.manifests.length, 1);

  const name = "multica-desktop-0.10.0-windows-x64.exe";
  const index = f.assets.findIndex((asset) => asset.name === name);
  const contents = "R".repeat(f.assets[index].size);
  f.assets.splice(index, 1);
  const replacement = f.add(name, contents);
  assert.equal((await promoteRelease(f.options)).status, "skipped");
  assert.equal(f.manifests.length, 2);
  const updated = f.manifests[1].split("\n").filter((line) => line.endsWith(`  ${name}`));
  assert.deepEqual(updated, [`${replacement.digest.slice("sha256:".length)}  ${name}`]);
  assert.deepEqual(f.calls, [f.tag]);
});

test("missing asset digests block checksum publication until available", async () => {
  const f = fixture();
  const asset = f.assets.find(({ name }) => name.endsWith(".dmg"));
  const digest = asset.digest;
  for (const invalid of [undefined, "sha512:abcd", "sha256:abcd"]) {
    asset.digest = invalid;
    assert.deepEqual(await promoteRelease(f.options), {
      status: "waiting",
      problems: [`Missing or invalid SHA-256 digest for ${asset.name}`],
    });
  }
  assert.deepEqual(f.manifests, []);
  assert.deepEqual(f.calls, []);
  asset.digest = digest;
  assert.equal((await promoteRelease(f.options)).status, "promoted");
});

test("a failed checksum replacement can retry without the old manifest", async () => {
  const f = fixture();
  f.add("checksums.txt");
  const uploadChecksums = f.options.uploadChecksums;
  f.options.uploadChecksums = async () => {
    f.assets.pop();
    throw new Error("upload failed after deleting the old manifest");
  };
  await assert.rejects(promoteRelease(f.options), /upload failed/);
  assert.deepEqual(f.calls, []);
  f.options.uploadChecksums = uploadChecksums;
  assert.equal((await promoteRelease(f.options)).status, "promoted");
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
  for (const key of ["getRelease", "getAssets", "readMetadata", "uploadChecksums", "getLatest", "promote"]) {
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
