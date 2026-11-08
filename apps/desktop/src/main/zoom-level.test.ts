// @vitest-environment node
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { ZOOM_MAX, ZOOM_MIN } from "./keyboard-shortcuts";
import {
  loadZoomLevel,
  parseZoomLevel,
  saveZoomLevel,
  zoomLevelFilePath,
} from "./zoom-level";

const dirs: string[] = [];
function tempFile(): string {
  const dir = mkdtempSync(join(tmpdir(), "multica-zoom-"));
  dirs.push(dir);
  return zoomLevelFilePath(dir);
}
afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

describe("parseZoomLevel", () => {
  it("reads a saved level", () => {
    expect(parseZoomLevel('{"level":1.5}')).toBe(1.5);
    expect(parseZoomLevel('{"level":-1}')).toBe(-1);
  });

  it("clamps to the shortcut zoom range", () => {
    expect(parseZoomLevel('{"level":99}')).toBe(ZOOM_MAX);
    expect(parseZoomLevel('{"level":-99}')).toBe(ZOOM_MIN);
  });

  it.each(["", "not json", "null", "[]", "{}", '{"level":"2"}', '{"level":null}'])(
    "falls back to 100%% for %j",
    (raw) => {
      expect(parseZoomLevel(raw)).toBe(0);
    },
  );
});

describe("loadZoomLevel / saveZoomLevel", () => {
  it("round-trips through the file", () => {
    const file = tempFile();
    saveZoomLevel(file, 2);
    expect(loadZoomLevel(file)).toBe(2);
  });

  it("returns 100% when the file is missing or corrupt", () => {
    const file = tempFile();
    expect(loadZoomLevel(file)).toBe(0);
    writeFileSync(file, "{", "utf8");
    expect(loadZoomLevel(file)).toBe(0);
  });

  it("does not throw when the file cannot be written", () => {
    expect(() => saveZoomLevel(join(tempFile(), "missing-dir", "x.json"), 1)).not.toThrow();
  });
});
