/**
 * Standalone node:test harness for window-state (no vitest/pnpm required).
 * Mirrors the vitest suite so CI and offline environments can both verify #5244.
 *
 * Run: node --experimental-strip-types --test src/main/window-state.node-test.mjs
 */
import { test, describe } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync, writeFileSync, readFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

const modUrl = pathToFileURL(join(import.meta.dirname, "window-state.ts")).href;
const {
  parseWindowState,
  loadWindowState,
  saveWindowStateToFile,
  isVisibleOnSomeDisplay,
  resolveWindowOptions,
  snapshotWindowState,
  windowStateFilePath,
  DEFAULT_WINDOW_WIDTH,
  DEFAULT_WINDOW_HEIGHT,
  MIN_WINDOW_WIDTH,
  WINDOW_STATE_FILENAME,
} = await import(modUrl);

const dirs = [];
function tempDir() {
  const dir = mkdtempSync(join(tmpdir(), "window-state-"));
  dirs.push(dir);
  return dir;
}
function cleanup() {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true });
}

describe("parseWindowState", () => {
  test("returns empty object on corrupt JSON", () => {
    assert.deepEqual(parseWindowState("{ not json"), {});
  });
  test("keeps only finite numbers and booleans", () => {
    assert.deepEqual(
      parseWindowState(
        JSON.stringify({
          x: 10.6,
          y: 20,
          width: 1400,
          height: 900,
          isMaximized: true,
          isFullScreen: false,
          junk: "nope",
        }),
      ),
      {
        x: 11,
        y: 20,
        width: 1400,
        height: 900,
        isMaximized: true,
        isFullScreen: false,
      },
    );
  });
});

describe("load/save window state file", () => {
  test("round-trips through disk", () => {
    try {
      const file = join(tempDir(), WINDOW_STATE_FILENAME);
      saveWindowStateToFile(file, {
        x: 1,
        y: 2,
        width: 1000,
        height: 700,
        isMaximized: true,
      });
      assert.equal(existsSync(file), true);
      assert.deepEqual(loadWindowState(file), {
        x: 1,
        y: 2,
        width: 1000,
        height: 700,
        isMaximized: true,
      });
    } finally {
      cleanup();
    }
  });
  test("load returns {} when file is missing", () => {
    try {
      assert.deepEqual(loadWindowState(join(tempDir(), "missing.json")), {});
    } finally {
      cleanup();
    }
  });
  test("load returns {} on corrupt file", () => {
    try {
      const file = join(tempDir(), "bad.json");
      writeFileSync(file, "{ broken", "utf8");
      assert.deepEqual(loadWindowState(file), {});
    } finally {
      cleanup();
    }
  });
});

describe("isVisibleOnSomeDisplay", () => {
  const displays = [{ x: 0, y: 0, width: 1920, height: 1080 }];
  test("false when position missing", () => {
    assert.equal(isVisibleOnSomeDisplay({ width: 100, height: 100 }, displays), false);
  });
  test("true when bounds intersect", () => {
    assert.equal(
      isVisibleOnSomeDisplay({ x: 100, y: 100, width: 800, height: 600 }, displays),
      true,
    );
  });
  test("false when off-screen", () => {
    assert.equal(
      isVisibleOnSomeDisplay({ x: 5000, y: 0, width: 800, height: 600 }, displays),
      false,
    );
  });
});

describe("resolveWindowOptions", () => {
  test("defaults when empty", () => {
    assert.deepEqual(resolveWindowOptions({}, false), {
      width: DEFAULT_WINDOW_WIDTH,
      height: DEFAULT_WINDOW_HEIGHT,
      isMaximized: false,
      isFullScreen: false,
    });
  });
  test("clamps below-minimum sizes", () => {
    const opts = resolveWindowOptions({ width: 100, height: 50 }, false);
    assert.equal(opts.width, MIN_WINDOW_WIDTH);
    assert.ok(opts.height >= 600);
  });
  test("includes position only when usePosition is true", () => {
    const withPos = resolveWindowOptions({ x: 40, y: 50, width: 1200, height: 800 }, true);
    assert.equal(withPos.x, 40);
    assert.equal(withPos.y, 50);
    const noPos = resolveWindowOptions({ x: 40, y: 50, width: 1200, height: 800 }, false);
    assert.equal(noPos.x, undefined);
    assert.equal(noPos.y, undefined);
  });
});

describe("snapshotWindowState", () => {
  test("null for destroyed windows", () => {
    assert.equal(
      snapshotWindowState({
        isDestroyed: () => true,
        getNormalBounds: () => ({ x: 0, y: 0, width: 1, height: 1 }),
        isMaximized: () => false,
        isFullScreen: () => false,
      }),
      null,
    );
  });
  test("captures normal bounds and flags", () => {
    assert.deepEqual(
      snapshotWindowState({
        isDestroyed: () => false,
        getNormalBounds: () => ({ x: 12, y: 34, width: 1100, height: 720 }),
        isMaximized: () => true,
        isFullScreen: () => false,
      }),
      {
        x: 12,
        y: 34,
        width: 1100,
        height: 720,
        isMaximized: true,
        isFullScreen: false,
      },
    );
  });
});

describe("windowStateFilePath", () => {
  test("joins userData with filename", () => {
    assert.equal(
      windowStateFilePath("/tmp/user-data"),
      join("/tmp/user-data", WINDOW_STATE_FILENAME),
    );
  });
});

describe("end-to-end persistence shape", () => {
  test("serializes a snapshot the loader accepts", () => {
    try {
      const file = join(tempDir(), WINDOW_STATE_FILENAME);
      const snap = snapshotWindowState({
        isDestroyed: () => false,
        getNormalBounds: () => ({ x: 5, y: 6, width: 1300, height: 850 }),
        isMaximized: () => false,
        isFullScreen: () => true,
      });
      assert.ok(snap);
      saveWindowStateToFile(file, snap);
      const raw = readFileSync(file, "utf8");
      assert.deepEqual(JSON.parse(raw), snap);
      assert.deepEqual(loadWindowState(file), snap);
    } finally {
      cleanup();
    }
  });
});
