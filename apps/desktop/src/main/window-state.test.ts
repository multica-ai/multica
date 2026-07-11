import { afterEach, describe, expect, it } from "vitest";
import { mkdtempSync, rmSync, writeFileSync, readFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import {
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
} from "./window-state";

const dirs: string[] = [];
function tempDir(): string {
  const dir = mkdtempSync(join(tmpdir(), "window-state-"));
  dirs.push(dir);
  return dir;
}

afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

describe("parseWindowState", () => {
  it("returns empty object on corrupt JSON", () => {
    expect(parseWindowState("{ not json")).toEqual({});
  });

  it("returns empty object on non-object JSON", () => {
    expect(parseWindowState("[]")).toEqual({});
    expect(parseWindowState("null")).toEqual({});
  });

  it("keeps only finite numbers and booleans", () => {
    expect(
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
    ).toEqual({
      x: 11,
      y: 20,
      width: 1400,
      height: 900,
      isMaximized: true,
      isFullScreen: false,
    });
  });
});

describe("load/save window state file", () => {
  it("round-trips through disk", () => {
    const file = join(tempDir(), WINDOW_STATE_FILENAME);
    saveWindowStateToFile(file, { x: 1, y: 2, width: 1000, height: 700, isMaximized: true });
    expect(existsSync(file)).toBe(true);
    expect(loadWindowState(file)).toEqual({
      x: 1,
      y: 2,
      width: 1000,
      height: 700,
      isMaximized: true,
    });
  });

  it("load returns {} when file is missing", () => {
    expect(loadWindowState(join(tempDir(), "missing.json"))).toEqual({});
  });

  it("load returns {} on corrupt file", () => {
    const file = join(tempDir(), "bad.json");
    writeFileSync(file, "{ broken", "utf8");
    expect(loadWindowState(file)).toEqual({});
  });
});

describe("isVisibleOnSomeDisplay", () => {
  const displays = [{ x: 0, y: 0, width: 1920, height: 1080 }];

  it("is false when position is missing", () => {
    expect(isVisibleOnSomeDisplay({ width: 100, height: 100 }, displays)).toBe(false);
  });

  it("is true when bounds intersect a display", () => {
    expect(isVisibleOnSomeDisplay({ x: 100, y: 100, width: 800, height: 600 }, displays)).toBe(
      true,
    );
  });

  it("is false when bounds are entirely off-screen (disconnected monitor)", () => {
    expect(
      isVisibleOnSomeDisplay({ x: 5000, y: 0, width: 800, height: 600 }, displays),
    ).toBe(false);
  });
});

describe("resolveWindowOptions", () => {
  it("uses defaults when saved state is empty", () => {
    expect(resolveWindowOptions({}, false)).toEqual({
      width: DEFAULT_WINDOW_WIDTH,
      height: DEFAULT_WINDOW_HEIGHT,
      isMaximized: false,
      isFullScreen: false,
    });
  });

  it("clamps below-minimum sizes", () => {
    const opts = resolveWindowOptions({ width: 100, height: 50 }, false);
    expect(opts.width).toBe(MIN_WINDOW_WIDTH);
    expect(opts.height).toBeGreaterThanOrEqual(600);
  });

  it("includes position only when usePosition is true", () => {
    const withPos = resolveWindowOptions({ x: 40, y: 50, width: 1200, height: 800 }, true);
    expect(withPos.x).toBe(40);
    expect(withPos.y).toBe(50);

    const noPos = resolveWindowOptions({ x: 40, y: 50, width: 1200, height: 800 }, false);
    expect(noPos.x).toBeUndefined();
    expect(noPos.y).toBeUndefined();
  });

  it("restores maximized / fullscreen flags", () => {
    const opts = resolveWindowOptions({ isMaximized: true, isFullScreen: true }, false);
    expect(opts.isMaximized).toBe(true);
    expect(opts.isFullScreen).toBe(true);
  });
});

describe("snapshotWindowState", () => {
  it("returns null for destroyed windows", () => {
    expect(
      snapshotWindowState({
        isDestroyed: () => true,
        getNormalBounds: () => ({ x: 0, y: 0, width: 1, height: 1 }),
        isMaximized: () => false,
        isFullScreen: () => false,
      }),
    ).toBeNull();
  });

  it("captures normal bounds and flags", () => {
    expect(
      snapshotWindowState({
        isDestroyed: () => false,
        getNormalBounds: () => ({ x: 12, y: 34, width: 1100, height: 720 }),
        isMaximized: () => true,
        isFullScreen: () => false,
      }),
    ).toEqual({
      x: 12,
      y: 34,
      width: 1100,
      height: 720,
      isMaximized: true,
      isFullScreen: false,
    });
  });
});

describe("windowStateFilePath", () => {
  it("joins userData with the canonical filename", () => {
    expect(windowStateFilePath("/tmp/user-data")).toBe(join("/tmp/user-data", WINDOW_STATE_FILENAME));
  });
});

// Sanity: write + re-parse via the real JSON format used on disk.
describe("end-to-end persistence shape", () => {
  it("serializes a snapshot the loader accepts", () => {
    const file = join(tempDir(), WINDOW_STATE_FILENAME);
    const snap = snapshotWindowState({
      isDestroyed: () => false,
      getNormalBounds: () => ({ x: 5, y: 6, width: 1300, height: 850 }),
      isMaximized: () => false,
      isFullScreen: () => true,
    });
    expect(snap).not.toBeNull();
    saveWindowStateToFile(file, snap!);
    const raw = readFileSync(file, "utf8");
    expect(JSON.parse(raw)).toEqual(snap);
    expect(loadWindowState(file)).toEqual(snap);
  });
});
