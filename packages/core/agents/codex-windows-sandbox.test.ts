import { describe, expect, it } from "vitest";
import type { RuntimeDevice } from "../types";
import { ensureCodexWindowsSandboxArgs } from "./codex-windows-sandbox";

function runtime(
  provider: string,
  os: string,
  configured = false,
): Pick<RuntimeDevice, "provider" | "metadata"> {
  return {
    provider,
    metadata: {
      os,
      codex_windows_sandbox_arg_configured: configured,
    },
  };
}

describe("ensureCodexWindowsSandboxArgs", () => {
  it("prepends the Windows Codex default as exactly two managed argv tokens", () => {
    expect(
      ensureCodexWindowsSandboxArgs(
        ["--profile", "research"],
        runtime("codex", "windows"),
      ),
    ).toEqual([
      "-c",
      'windows.sandbox="unelevated"',
      "--profile",
      "research",
    ]);
  });

  it("keeps the managed prefix idempotent", () => {
    const args = [
      "-c",
      'windows.sandbox="unelevated"',
      "--profile",
      "research",
    ];
    expect(
      ensureCodexWindowsSandboxArgs(args, runtime("codex", "windows")),
    ).toEqual(args);
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

  it("removes the managed prefix when runtime arguments own the setting", () => {
    expect(
      ensureCodexWindowsSandboxArgs(
        [
          "-c",
          'windows.sandbox="unelevated"',
          "--profile",
          "research",
        ],
        runtime("codex", "windows", true),
      ),
    ).toEqual(["--profile", "research"]);
  });

  it("leaves unrelated non-Windows and non-Codex arguments unchanged", () => {
    const args = ["--profile", "research"];
    expect(
      ensureCodexWindowsSandboxArgs(args, runtime("codex", "linux")),
    ).toEqual(args);
    expect(
      ensureCodexWindowsSandboxArgs(args, runtime("claude", "windows")),
    ).toEqual(args);
  });

  it("removes a stale managed prefix outside Windows Codex", () => {
    const args = [
      "-c",
      'windows.sandbox="unelevated"',
      "--profile",
      "research",
    ];
    expect(
      ensureCodexWindowsSandboxArgs(args, runtime("codex", "linux")),
    ).toEqual(["--profile", "research"]);
    expect(
      ensureCodexWindowsSandboxArgs(args, runtime("claude", "windows")),
    ).toEqual(["--profile", "research"]);
  });
});
