import { describe, expect, it } from "vitest";
import type { RuntimeDevice } from "../types";
import { ensureCodexWindowsSandboxArgs } from "./codex-windows-sandbox";

function runtime(
  provider: string,
  os: string,
): Pick<RuntimeDevice, "provider" | "metadata"> {
  return { provider, metadata: { os } };
}

describe("ensureCodexWindowsSandboxArgs", () => {
  it("appends the Windows Codex default as exactly two argv tokens", () => {
    expect(
      ensureCodexWindowsSandboxArgs(
        ["--profile", "research"],
        runtime("codex", "windows"),
      ),
    ).toEqual([
      "--profile",
      "research",
      "-c",
      'windows.sandbox="unelevated"',
    ]);
  });

  it("preserves an explicit override without adding a duplicate", () => {
    expect(
      ensureCodexWindowsSandboxArgs(
        ["-c", 'windows.sandbox="elevated"', "--profile", "research"],
        runtime("codex", "windows"),
      ),
    ).toEqual([
      "-c",
      'windows.sandbox="elevated"',
      "--profile",
      "research",
    ]);
  });

  it("detects inline and shell-quoted overrides", () => {
    const args = ["'--config=windows.sandbox=\"elevated\"'"];
    expect(
      ensureCodexWindowsSandboxArgs(args, runtime("codex", "windows")),
    ).toEqual(args);
  });

  it("leaves non-Windows and non-Codex arguments unchanged", () => {
    const args = ["--profile", "research"];
    expect(
      ensureCodexWindowsSandboxArgs(args, runtime("codex", "linux")),
    ).toEqual(args);
    expect(
      ensureCodexWindowsSandboxArgs(args, runtime("claude", "windows")),
    ).toEqual(args);
  });
});
