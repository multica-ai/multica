import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { deflateSync } from "node:zlib";

import { changedFiles, decodePng, flattenPng, parseCli, pickWindow, swapToBase } from "./desktop-window-capture.mjs";

const CRC_TABLE = Array.from({ length: 256 }, (_, n) => {
  let c = n;
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});

function chunk(type, data) {
  const body = Buffer.concat([Buffer.from(type, "latin1"), data]);
  let c = 0xffffffff;
  for (const byte of body) c = CRC_TABLE[(c ^ byte) & 0xff] ^ (c >>> 8);
  const out = Buffer.alloc(12 + data.length);
  out.writeUInt32BE(data.length, 0);
  body.copy(out, 4);
  out.writeUInt32BE((c ^ 0xffffffff) >>> 0, 8 + data.length);
  return out;
}

// Encodes RGBA rows, using a different PNG filter on each row so the decoder's
// five filter paths are all exercised.
function rgbaPng(width, rows, extraChunks = []) {
  const stride = width * 4;
  const raw = [];
  let previous = Buffer.alloc(stride);
  rows.forEach((pixels, y) => {
    const row = Buffer.from(pixels.flat());
    const filter = y % 5;
    const filtered = Buffer.alloc(stride);
    for (let x = 0; x < stride; x++) {
      const left = x >= 4 ? row[x - 4] : 0;
      const up = previous[x];
      const upLeft = x >= 4 ? previous[x - 4] : 0;
      const p = left + up - upLeft;
      const [pa, pb, pc] = [Math.abs(p - left), Math.abs(p - up), Math.abs(p - upLeft)];
      const predictor = [0, left, up, (left + up) >> 1, pa <= pb && pa <= pc ? left : pb <= pc ? up : upLeft][filter];
      filtered[x] = (row[x] - predictor) & 0xff;
    }
    raw.push(Buffer.from([filter]), filtered);
    previous = row;
  });
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(width, 0);
  ihdr.writeUInt32BE(rows.length, 4);
  ihdr[8] = 8;
  ihdr[9] = 6;
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    ...extraChunks,
    chunk("IDAT", deflateSync(Buffer.concat(raw))),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

test("flattening puts transparent corners on white and keeps opaque pixels exact", () => {
  const clear = [0, 0, 0, 0];
  const red = [200, 30, 40, 255];
  const halfBlack = [0, 0, 0, 128];
  const rows = Array.from({ length: 6 }, (_, y) => [y === 0 ? clear : red, halfBlack, [10 * y, 20, 30, 255]]);
  const profile = chunk("iCCP", Buffer.from("Display P3\0\0profile-bytes"));
  const appleIndex = chunk("iDOT", Buffer.alloc(28));
  const { png, width, height } = flattenPng(rgbaPng(3, rows, [profile, appleIndex]));
  assert.deepEqual([width, height], [3, 6]);

  const flat = decodePng(png);
  assert.equal(flat.channels, 3);
  const pixel = (x, y) => [...flat.pixels.subarray((y * 3 + x) * 3, (y * 3 + x) * 3 + 3)];
  assert.deepEqual(pixel(0, 0), [255, 255, 255]);
  assert.deepEqual(pixel(0, 1), [200, 30, 40]);
  assert.deepEqual(pixel(1, 3), [127, 127, 127]);
  for (let y = 0; y < 6; y++) assert.deepEqual(pixel(2, y), [10 * y, 20, 30]);

  const types = flat.chunks.map((c) => c.type);
  assert.ok(types.includes("iCCP"), "the colour profile survives");
  assert.ok(!types.includes("iDOT"), "Apple's IDAT index is dropped because it points into the old IDAT");
});

test("an already opaque PNG is passed through unchanged", () => {
  const { png } = flattenPng(rgbaPng(2, [[[1, 2, 3, 255], [4, 5, 6, 255]]]));
  assert.deepEqual(flattenPng(png).png, png);
});

test("only the CDP owner's visible, full-size windows are candidates", () => {
  const window = (id, overrides = {}) => ({
    id,
    pid: 100,
    layer: 0,
    onscreen: true,
    alpha: 1,
    title: "Issues",
    bounds: { X: 0, Y: 0, Width: 1280, Height: 800 },
    ...overrides,
  });
  const windows = [
    window(1, { pid: 200, title: "Issues" }),
    window(2, { bounds: { X: 0, Y: 0, Width: 2880, Height: 30 }, title: "" }),
    window(3, { onscreen: false }),
    window(4, { layer: 25 }),
    window(5, { title: "DEV-2" }),
    window(6, { title: "Issues" }),
  ];
  const picked = pickWindow(windows, 100);
  assert.equal(picked.window.id, 5);
  assert.deepEqual(picked.others.map((w) => w.id), [6]);
  assert.equal(pickWindow(windows, 100, "Issues").window.id, 6);
  assert.equal(pickWindow(windows, 300).window, null);
});

test("compare swaps changed files to BASE without touching the index, then restores them exactly", (t) => {
  const repo = mkdtempSync(join(tmpdir(), "desktop-window-repo-"));
  const backup = mkdtempSync(join(tmpdir(), "desktop-window-backup-"));
  t.after(() => {
    rmSync(repo, { recursive: true, force: true });
    rmSync(backup, { recursive: true, force: true });
  });
  const env = {
    ...process.env,
    GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null",
    GIT_AUTHOR_NAME: "CI test", GIT_AUTHOR_EMAIL: "ci@example.invalid",
    GIT_COMMITTER_NAME: "CI test", GIT_COMMITTER_EMAIL: "ci@example.invalid",
  };
  const git = (...args) => execFileSync("git", args, { cwd: repo, env, encoding: "utf8" }).trim();
  git("init", "-b", "main");
  writeFileSync(join(repo, "modified.ts"), "base\n");
  writeFileSync(join(repo, "deleted.ts"), "only at base\n");
  writeFileSync(join(repo, "untouched.ts"), "same\n");
  git("add", ".");
  git("commit", "-m", "base");
  const base = git("rev-parse", "HEAD");

  writeFileSync(join(repo, "modified.ts"), "committed change\n");
  git("rm", "-q", "deleted.ts");
  writeFileSync(join(repo, "added.ts"), "new\n");
  git("add", ".");
  git("commit", "-m", "head");
  writeFileSync(join(repo, "modified.ts"), "uncommitted change\n");
  chmodSync(join(repo, "modified.ts"), 0o755);
  writeFileSync(join(repo, "untracked.ts"), "scratch\n");
  writeFileSync(join(repo, "added.ts"), "staged\n");
  git("add", "added.ts");
  const indexBefore = git("diff", "--cached", "--name-status");

  const files = changedFiles(repo, base);
  assert.deepEqual(
    files.sort((a, b) => a.path.localeCompare(b.path)),
    [
      { status: "A", path: "added.ts" },
      { status: "D", path: "deleted.ts" },
      { status: "M", path: "modified.ts" },
    ],
  );
  assert.deepEqual(changedFiles(repo, base, ["modified.ts"]), [{ status: "M", path: "modified.ts" }]);

  const restore = swapToBase(repo, base, files, backup);
  assert.equal(readFileSync(join(repo, "modified.ts"), "utf8"), "base\n");
  assert.equal(readFileSync(join(repo, "deleted.ts"), "utf8"), "only at base\n");
  assert.equal(existsSync(join(repo, "added.ts")), false);
  assert.equal(readFileSync(join(repo, "untracked.ts"), "utf8"), "scratch\n");
  assert.equal(git("diff", "--cached", "--name-status"), indexBefore);
  assert.ok(existsSync(join(backup, "files.json")), "the backup can be recovered by hand");

  restore();
  assert.equal(readFileSync(join(repo, "modified.ts"), "utf8"), "uncommitted change\n");
  assert.equal(statSync(join(repo, "modified.ts")).mode & 0o777, 0o755);
  assert.equal(readFileSync(join(repo, "added.ts"), "utf8"), "staged\n");
  assert.equal(existsSync(join(repo, "deleted.ts")), false);
  assert.equal(git("diff", "--cached", "--name-status"), indexBefore);
  assert.deepEqual(changedFiles(repo, base), files);
});

test("pathspecs only make sense with --compare", () => {
  assert.throws(() => parseCli(["out.png", "--", "apps/desktop"]), /only applies to --compare/);
  assert.deepEqual(parseCli(["--compare", "main", "shots", "--", "apps/desktop"]).pathspecs, ["apps/desktop"]);
  assert.throws(() => parseCli(["--settle", "-1", "out.png"]), /--settle/);
});
