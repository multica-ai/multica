import { describe, expect, it } from "vitest";
import {
  applyPathToEnv,
  readEnvPath,
  resolveEnvPathKey,
  withAliasDirOnPath,
} from "./env-path";

describe("resolveEnvPathKey", () => {
  it("returns the existing Path key on Windows-style env", () => {
    expect(resolveEnvPathKey({ Path: "C:\\bin" }, "win32")).toBe("Path");
  });

  it("defaults to Path on win32 when absent", () => {
    expect(resolveEnvPathKey({}, "win32")).toBe("Path");
  });

  it("defaults to PATH on posix when absent", () => {
    expect(resolveEnvPathKey({}, "linux")).toBe("PATH");
  });
});

describe("withAliasDirOnPath", () => {
  it("prepends the alias when PATH is present", () => {
    expect(
      withAliasDirOnPath("C:\\Users\\x\\npm;C:\\Windows", "C:\\alias", "win32"),
    ).toBe("C:\\alias;C:\\Users\\x\\npm;C:\\Windows");
  });

  it("does not degrade an empty PATH to alias-only", () => {
    expect(withAliasDirOnPath("", "C:\\alias", "win32")).toBe("");
    expect(withAliasDirOnPath(undefined, "C:\\alias", "win32")).toBeUndefined();
  });

  it("is a no-op when alias is already first", () => {
    expect(
      withAliasDirOnPath("C:\\alias;C:\\Windows", "C:\\alias", "win32"),
    ).toBe("C:\\alias;C:\\Windows");
  });
});

describe("applyPathToEnv", () => {
  it("collapses Path + PATH duplicates into a single Path key", () => {
    const env = applyPathToEnv(
      {
        Path: "C:\\Users\\x\\AppData\\Roaming\\npm;C:\\Windows\\System32",
        PATH: "C:\\alias-only",
        MULTICA_LAUNCHED_BY: "desktop",
      },
      { platform: "win32" },
    );

    const pathKeys = Object.keys(env).filter((k) => k.toUpperCase() === "PATH");
    expect(pathKeys).toEqual(["Path"]);
    expect(env.Path).toBe(
      "C:\\Users\\x\\AppData\\Roaming\\npm;C:\\Windows\\System32",
    );
    expect(env).not.toHaveProperty("PATH");
    expect(env.MULTICA_LAUNCHED_BY).toBe("desktop");
  });

  it("prepends alias onto the surviving PATH without dropping user entries", () => {
    const env = applyPathToEnv(
      { Path: "C:\\npm;C:\\Windows" },
      { aliasDir: "C:\\compat-bin", platform: "win32" },
    );
    expect(env.Path).toBe("C:\\compat-bin;C:\\npm;C:\\Windows");
  });

  it("refuses to replace a missing PATH with alias-only", () => {
    const env = applyPathToEnv(
      { MULTICA_LAUNCHED_BY: "desktop" },
      { aliasDir: "C:\\compat-bin", platform: "win32" },
    );
    expect(env).not.toHaveProperty("Path");
    expect(env).not.toHaveProperty("PATH");
  });
});

describe("readEnvPath", () => {
  it("reads whichever PATH casing is present", () => {
    expect(readEnvPath({ Path: "C:\\bin" })).toBe("C:\\bin");
    expect(readEnvPath({ PATH: "/usr/bin" })).toBe("/usr/bin");
  });
});
