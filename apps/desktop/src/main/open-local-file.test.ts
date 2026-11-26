// @vitest-environment node
import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({
  ipcMain: { handle: vi.fn() },
  shell: { showItemInFolder: vi.fn(), openExternal: vi.fn() },
}));

import { shell } from "electron";
import {
  detectInstalledOpeners,
  isSafeAbsoluteLocalPath,
  openLocalFile,
  pickDefaultOpenerId,
  resolveLocalFilePath,
  type OpenLocalFileDeps,
} from "./open-local-file";

function deps(partial: Partial<OpenLocalFileDeps> = {}): OpenLocalFileDeps {
  return {
    platform: "darwin",
    homedir: () => "/Users/dev",
    env: {},
    pathExists: async () => false,
    which: async () => null,
    execFile: async () => undefined,
    showItemInFolder: vi.fn(),
    ...partial,
  };
}

describe("isSafeAbsoluteLocalPath", () => {
  it("allows native absolute filesystem paths", () => {
    expect(isSafeAbsoluteLocalPath("/Users/dev/src/a.ts", "darwin")).toBe(true);
    expect(isSafeAbsoluteLocalPath("C:\\Users\\dev\\src\\a.ts", "win32")).toBe(true);
    expect(isSafeAbsoluteLocalPath("\\\\server\\share\\a.ts", "win32")).toBe(true);
  });

  it("rejects relative paths, URLs, and editor protocol handlers", () => {
    expect(isSafeAbsoluteLocalPath("src/a.ts", "darwin")).toBe(false);
    expect(isSafeAbsoluteLocalPath("vscode://file/test", "darwin")).toBe(false);
    expect(isSafeAbsoluteLocalPath("file:///Users/dev/a.ts", "darwin")).toBe(false);
    expect(isSafeAbsoluteLocalPath("https://example.com/a.ts", "darwin")).toBe(false);
    expect(isSafeAbsoluteLocalPath("", "darwin")).toBe(false);
    expect(isSafeAbsoluteLocalPath("/tmp/has\0nul", "darwin")).toBe(false);
  });

  it("does not treat a Windows path as local on macOS/Linux", () => {
    expect(isSafeAbsoluteLocalPath("C:\\Users\\dev\\a.ts", "darwin")).toBe(false);
  });
});

describe("detectInstalledOpeners", () => {
  it("lists only installed editors plus the file manager", async () => {
    const openers = await detectInstalledOpeners(
      deps({
        pathExists: async (path) =>
          path === "/Applications/Cursor.app" ||
          path === "/Applications/Visual Studio Code.app",
      }),
    );
    expect(openers.map((item) => item.id)).toEqual([
      "cursor",
      "vscode",
      "file-manager",
    ]);
    expect(openers.find((item) => item.id === "cursor")?.name).toBe("Cursor");
  });

  it("does not list Zed or JetBrains when they are not installed", async () => {
    const openers = await detectInstalledOpeners(
      deps({
        pathExists: async (path) => path === "/Applications/Cursor.app",
      }),
    );
    expect(openers.some((item) => item.id === "zed")).toBe(false);
    expect(openers.some((item) => item.id.startsWith("jetbrains-"))).toBe(false);
  });

  it("falls back to PATH CLIs on Linux", async () => {
    const openers = await detectInstalledOpeners(
      deps({
        platform: "linux",
        which: async (command) => (command === "code" ? "/usr/bin/code" : null),
      }),
    );
    expect(openers.map((item) => item.id)).toEqual(["vscode", "file-manager"]);
  });
});

describe("pickDefaultOpenerId", () => {
  it("prefers Cursor, then VS Code, then the file manager", () => {
    expect(
      pickDefaultOpenerId([
        { id: "vscode", kind: "editor", name: "Visual Studio Code" },
        { id: "file-manager", kind: "file_manager", name: "File manager" },
      ]),
    ).toBe("vscode");
    expect(
      pickDefaultOpenerId([
        { id: "cursor", kind: "editor", name: "Cursor" },
        { id: "vscode", kind: "editor", name: "Visual Studio Code" },
        { id: "file-manager", kind: "file_manager", name: "File manager" },
      ]),
    ).toBe("cursor");
    expect(
      pickDefaultOpenerId([
        { id: "file-manager", kind: "file_manager", name: "File manager" },
      ]),
    ).toBe("file-manager");
  });
});

describe("resolveLocalFilePath", () => {
  it("accepts an existing absolute path", async () => {
    const result = await resolveLocalFilePath("/Users/dev/src/a.ts", {
      platform: "darwin",
      pathExists: async (path) => path === "/Users/dev/src/a.ts",
    });
    expect(result).toEqual({ ok: true, path: "/Users/dev/src/a.ts" });
  });

  it("rejects vscode:// and other URLs instead of handing them to the OS", async () => {
    const result = await resolveLocalFilePath("vscode://file/test", {
      platform: "darwin",
      pathExists: async () => true,
    });
    expect(result).toEqual({ ok: false, reason: "unsafe" });
  });

  it("rejects relative paths", async () => {
    const result = await resolveLocalFilePath("src/a.ts", {
      platform: "darwin",
      pathExists: async () => true,
    });
    expect(result).toEqual({ ok: false, reason: "not_absolute" });
  });

  it("rejects missing files", async () => {
    const result = await resolveLocalFilePath("/tmp/missing.ts", {
      platform: "darwin",
      pathExists: async () => false,
    });
    expect(result).toEqual({ ok: false, reason: "not_found" });
  });
});

describe("openLocalFile", () => {
  it("reveals the file in the file manager without openExternal", async () => {
    const showItemInFolder = vi.fn();
    const execFile = vi.fn(async () => undefined);
    const result = await openLocalFile(
      { path: "/Users/dev/src/a.ts", openerId: "file-manager" },
      deps({
        pathExists: async (path) =>
          path === "/Users/dev/src/a.ts" || path === "/Applications/Cursor.app",
        showItemInFolder,
        execFile,
      }),
    );
    expect(result).toEqual({ ok: true });
    expect(showItemInFolder).toHaveBeenCalledWith("/Users/dev/src/a.ts");
    expect(execFile).not.toHaveBeenCalled();
    expect(shell.openExternal).not.toHaveBeenCalled();
  });

  it("opens an editor via `open -a` on macOS", async () => {
    const execFile = vi.fn(async () => undefined);
    const result = await openLocalFile(
      { path: "/Users/dev/src/a.ts", openerId: "cursor" },
      deps({
        pathExists: async (path) =>
          path === "/Users/dev/src/a.ts" || path === "/Applications/Cursor.app",
        execFile,
      }),
    );
    expect(result).toEqual({ ok: true });
    expect(execFile).toHaveBeenCalledWith("open", [
      "-a",
      "Cursor",
      "--",
      "/Users/dev/src/a.ts",
    ]);
  });

  it("refuses an opener that is not currently installed", async () => {
    const result = await openLocalFile(
      { path: "/Users/dev/src/a.ts", openerId: "zed" },
      deps({
        pathExists: async (path) => path === "/Users/dev/src/a.ts",
      }),
    );
    expect(result).toEqual({ ok: false, reason: "opener_unavailable" });
  });

  it("surfaces launch failures instead of failing silently", async () => {
    const result = await openLocalFile(
      { path: "/Users/dev/src/a.ts", openerId: "cursor" },
      deps({
        pathExists: async (path) =>
          path === "/Users/dev/src/a.ts" || path === "/Applications/Cursor.app",
        execFile: async () => {
          throw new Error("spawn open ENOENT");
        },
      }),
    );
    expect(result).toEqual({
      ok: false,
      reason: "launch_failed",
      error: "spawn open ENOENT",
    });
  });
});
