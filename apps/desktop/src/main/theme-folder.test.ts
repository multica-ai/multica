// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "fs";
import { tmpdir } from "os";
import { join } from "path";
import { readThemeFolder, watchThemeFolder, writeThemeFolder } from "./theme-folder";

vi.mock("electron", () => ({
  app: { getPath: vi.fn(), on: vi.fn() },
  BrowserWindow: { getAllWindows: vi.fn(() => []) },
  ipcMain: { handle: vi.fn() },
  shell: { openPath: vi.fn() },
}));

let root: string;
let outside: string;
const text = (data: Uint8Array) => new TextDecoder().decode(data);
const bytes = (value: string) => new TextEncoder().encode(value);

function theme(dir: string, css = ".theme-light { --radius: 0; }") {
  mkdirSync(join(dir, "fonts"), { recursive: true });
  writeFileSync(join(dir, "manifest.json"), '{"apiVersion":1,"id":"paper","name":"Paper"}');
  writeFileSync(join(dir, "theme.css"), css);
  writeFileSync(join(dir, "fonts", "a.woff2"), "wOF2");
}

beforeEach(() => {
  root = mkdtempSync(join(tmpdir(), "themes-"));
  outside = mkdtempSync(join(tmpdir(), "theme-repo-"));
});
afterEach(() => {
  rmSync(root, { recursive: true, force: true });
  rmSync(outside, { recursive: true, force: true });
});

describe("readThemeFolder", () => {
  it("reads theme folders, linked folders and zips, and only the files a theme can use", async () => {
    theme(join(root, "paper"));
    mkdirSync(join(root, "paper", ".git"));
    writeFileSync(join(root, "paper", ".git", "HEAD"), "ref");
    writeFileSync(join(root, "paper", "notes.txt"), "not a theme file");
    theme(outside);
    symlinkSync(outside, join(root, "linked"));
    writeFileSync(join(root, "ink.zip"), "PK");
    writeFileSync(join(root, "README.md"), "ignored");

    const snapshot = await readThemeFolder(root);
    expect(snapshot.path).toBe(root);
    expect(snapshot.entries.map((e) => [e.folder, e.files?.map((f) => f.path).sort()])).toEqual([
      ["ink.zip", ["ink.zip"]],
      ["linked", ["fonts/a.woff2", "manifest.json", "theme.css"]],
      ["paper", ["fonts/a.woff2", "manifest.json", "theme.css"]],
    ]);
    expect(text(snapshot.entries[2]!.files!.find((f) => f.path === "theme.css")!.data)).toContain("--radius");
  });

  it("creates the folder and reports a theme that is too large", async () => {
    const fresh = join(root, "nested", "themes");
    expect(await readThemeFolder(fresh)).toEqual({ path: fresh, entries: [] });
    theme(join(root, "huge"), `/* ${"x".repeat(1024 * 1024)} */`);
    const snapshot = await readThemeFolder(root);
    expect(snapshot.entries.find((e) => e.folder === "huge")).toEqual({
      folder: "huge",
      error: expect.stringMatching(/at most 1024 KB/),
    });
  });
});

describe("writeThemeFolder", () => {
  const files = [
    { path: "manifest.json", data: bytes("{}") },
    { path: "theme.css", data: bytes("a {}") },
    { path: "fonts/a.woff2", data: bytes("wOF2") },
  ];

  it("writes a new folder and never replaces one", async () => {
    expect(await writeThemeFolder(root, "pop-copy", files)).toEqual({ ok: true });
    expect(readFileSync(join(root, "pop-copy", "fonts", "a.woff2"), "utf8")).toBe("wOF2");
    expect(await writeThemeFolder(root, "pop-copy", files)).toEqual({ ok: false, reason: "exists" });
  });

  it.each([
    ["a folder name outside the id rules", "../escape", files],
    ["a path that leaves the folder", "x", [{ path: "../x.css", data: bytes("") }]],
    ["an absolute path", "x", [{ path: "/etc/theme.css", data: bytes("") }]],
    ["a file a theme cannot use", "x", [{ path: "run.sh", data: bytes("") }]],
    ["data that is not bytes", "x", [{ path: "theme.css", data: "a {}" }]],
    ["no files", "x", []],
  ])("refuses %s", async (_case, name, list) => {
    expect(await writeThemeFolder(root, name, list)).toEqual({ ok: false, reason: "invalid" });
  });
});

describe("watchThemeFolder", () => {
  const changed = (onChange: () => void) =>
    new Promise<number>((resolve) => {
      const started = Date.now();
      const stop = watchThemeFolder(root, () => {
        stop();
        onChange();
        resolve(Date.now() - started);
      });
    });

  it("reports an edit inside a theme, also through a symlink, within a second", async () => {
    theme(outside);
    symlinkSync(outside, join(root, "linked"));
    const onChange = vi.fn();
    const settled = changed(onChange);
    await new Promise((r) => setTimeout(r, 200));
    writeFileSync(join(outside, "theme.css"), ".theme-light { --radius: 4px; }");
    expect(await settled).toBeLessThan(1000);
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it("reports a theme added to the folder", async () => {
    const settled = changed(() => {});
    await new Promise((r) => setTimeout(r, 200));
    theme(join(root, "paper"));
    expect(await settled).toBeLessThan(1000);
  });
});
