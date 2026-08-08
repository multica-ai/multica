import { describe, expect, it } from "vitest";
import { parseManagedRuntimeInstallResult } from "./managed-runtime-install-result";

describe("parseManagedRuntimeInstallResult", () => {
  it("accepts a validated managed Pi result", () => {
    expect(
      parseManagedRuntimeInstallResult(
        JSON.stringify({
          provider: "pi",
          version: "0.83.0",
          path: "/home/user/.multica/runtimes/pi/current/pi",
          source: "managed",
          installed: true,
        }),
        "pi",
      ),
    ).toEqual({
      provider: "pi",
      version: "0.83.0",
      path: "/home/user/.multica/runtimes/pi/current/pi",
      source: "managed",
      installed: true,
    });
  });

  it.each([
    "null",
    "[]",
    JSON.stringify({ provider: "codex", path: "/bin/pi", source: "user", installed: false }),
    JSON.stringify({ provider: "pi", path: "", source: "user", installed: false }),
    JSON.stringify({ provider: "pi", path: "/bin/pi", source: "remote", installed: false }),
  ])("rejects malformed or mismatched output: %s", (raw) => {
    expect(() => parseManagedRuntimeInstallResult(raw, "pi")).toThrow();
  });
});
