// @vitest-environment node
import { createRequire } from "node:module";
import { describe, expect, it, vi } from "vitest";

import { verifyMacRelease } from "./verify-mac-release.mjs";
const context = (publish = "always", platform = "darwin") => ({
  appOutDir: "/build path/mac-arm64",
  electronPlatformName: platform,
  packager: { info: { options: { publish } }, appInfo: { productFilename: "Multica" } },
});

describe("macOS release validation before upload", () => {
  it("loads through electron-builder's actual hook resolver", async () => {
    const require = createRequire(import.meta.url);
    const { resolveFunction } = createRequire(require.resolve("electron-builder"))("app-builder-lib/out/util/resolve");
    const hook = await resolveFunction("module", new URL("./verify-mac-release.mjs", import.meta.url).pathname, "afterSign");
    const execute = vi.fn();
    hook(context(), execute);
    expect(execute).toHaveBeenCalledTimes(2);
  });
  it.each(["always", "onTag", "onTagOrDraft"])("assesses the notarized app when publishing with %s", (policy) => {
    const execute = vi.fn();
    verifyMacRelease(context(policy), execute);
    expect(execute.mock.calls.map(([command, args]) => [command, args])).toEqual([
      ["spctl", ["--assess", "--type", "execute", "--verbose", "/build path/mac-arm64/Multica.app"]],
      ["xcrun", ["stapler", "validate", "/build path/mac-arm64/Multica.app"]],
    ]);
    expect(execute.mock.calls.every(([, , options]) => options.timeout === 60_000)).toBe(true);
  });

  it.each(["spctl", "xcrun"])("stops publication when %s fails", (failing) => {
    const execute = vi.fn((command) => { if (command === failing) throw new Error("validation failed"); });
    expect(() => verifyMacRelease(context(), execute)).toThrow("validation failed");
    if (failing === "spctl") expect(execute).toHaveBeenCalledTimes(1);
  });

  it("keeps unsigned local builds and other platforms unchanged", () => {
    const execute = vi.fn();
    for (const policy of ["never", ""]) verifyMacRelease(context(policy), execute);
    for (const platform of ["win32", "linux"]) verifyMacRelease(context("always", platform), execute);
    expect(execute).not.toHaveBeenCalled();
  });
});
