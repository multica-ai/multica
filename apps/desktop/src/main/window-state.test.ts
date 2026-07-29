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
  const displays = [{ x: 0, y: 0, width: 1920, height: 1080 }];

  it("uses defaults when saved state is empty", () => {
    expect(resolveWindowOptions({}, displays)).toEqual({
      width: DEFAULT_WINDOW_WIDTH,
      height: DEFAULT_WINDOW_HEIGHT,
      isMaximized: false,
      isFullScreen: false,
    });
  });

  it("clamps below-minimum sizes", () => {
    const opts = resolveWindowOptions({ width: 100, height: 50 }, displays);
    expect(opts.width).toBe(MIN_WINDOW_WIDTH);
    expect(opts.height).toBeGreaterThanOrEqual(600);
  });

  it("keeps an intersecting saved position", () => {
    const opts = resolveWindowOptions({ x: 40, y: 50, width: 1200, height: 800 }, displays);
    expect(opts.x).toBe(40);
    expect(opts.y).toBe(50);
  });

  it("clamps a one-pixel intersection fully inside the work area", () => {
    const opts = resolveWindowOptions(
      { x: 1919, y: 1079, width: 1200, height: 800 },
      displays,
    );
    expect(opts).toMatchObject({ x: 720, y: 280, width: 1200, height: 800 });
  });

  it("clamps position when restoring onto a smaller display", () => {
    const opts = resolveWindowOptions(
      { x: 500, y: 300, width: 900, height: 600 },
      [{ x: 0, y: 0, width: 1024, height: 768 }],
    );
    expect(opts).toMatchObject({ x: 124, y: 168, width: 900, height: 600 });
  });

  it("shrinks saved dimensions to fit the selected work area", () => {
    const opts = resolveWindowOptions(
      { x: 100, y: 100, width: 1600, height: 1000 },
      [{ x: 0, y: 0, width: 1024, height: 768 }],
    );
    expect(opts).toMatchObject({ x: 0, y: 0, width: 1024, height: 768 });
  });

  it("selects the work area with the largest intersection", () => {
    const opts = resolveWindowOptions(
      { x: 1800, y: 100, width: 1000, height: 800 },
      [
        { x: 0, y: 0, width: 1920, height: 1080 },
        { x: 1920, y: 0, width: 1280, height: 1024 },
      ],
    );
    expect(opts).toMatchObject({ x: 1920, y: 100, width: 1000, height: 800 });
  });

  it("omits coordinates when saved bounds are completely off-screen", () => {
    const opts = resolveWindowOptions(
      { x: 5000, y: 0, width: 1200, height: 800, isMaximized: true },
      displays,
    );
    expect(opts).toEqual({
      width: 1200,
      height: 800,
      isMaximized: true,
      isFullScreen: false,
    });
  });

  it("shrinks completely off-screen bounds to the primary work area", () => {
    const opts = resolveWindowOptions(
      { x: 5000, y: 0, width: 2000, height: 1200 },
      [{ x: 0, y: 0, width: 1440, height: 900 }],
    );
    // Size collapses onto the current work area; coordinates stay omitted so
    // Electron centers the window instead of restoring it off-screen.
    expect(opts).toEqual({
      width: 1440,
      height: 900,
      isMaximized: false,
      isFullScreen: false,
    });
  });

  it("clamps off-screen bounds to the supplied primary display, not just displays[0]", () => {
    const opts = resolveWindowOptions(
      { x: 9000, y: 9000, width: 2560, height: 1600 },
      [
        { x: 0, y: 0, width: 1920, height: 1080 },
        { x: 1920, y: 0, width: 1280, height: 800 },
      ],
      { x: 1920, y: 0, width: 1280, height: 800 },
    );
    expect(opts).toEqual({
      width: 1280,
      height: 800,
      isMaximized: false,
      isFullScreen: false,
    });
  });

  it("shrinks default dimensions when the work area is smaller than the default", () => {
    const opts = resolveWindowOptions({}, [{ x: 0, y: 0, width: 1024, height: 768 }]);
    expect(opts).toMatchObject({ width: 1024, height: 768 });
  });

  it("keeps saved size when no displays are reported", () => {
    const opts = resolveWindowOptions({ x: 0, y: 0, width: 1500, height: 950 }, []);
    expect(opts).toMatchObject({ width: 1500, height: 950 });
  });

  it("ignores degenerate work areas when picking the off-screen clamp target", () => {
    const opts = resolveWindowOptions(
      { x: 5000, y: 0, width: 2000, height: 1200 },
      [
        { x: 0, y: 0, width: 0, height: 0 },
        { x: 0, y: 0, width: 1366, height: 768 },
      ],
    );
    expect(opts).toMatchObject({ width: 1366, height: 768 });
  });

  it("restores maximized / fullscreen flags", () => {
    const opts = resolveWindowOptions({ isMaximized: true, isFullScreen: true }, displays);
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
