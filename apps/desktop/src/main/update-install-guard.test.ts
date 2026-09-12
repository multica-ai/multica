// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ execFile: vi.fn() }));
vi.mock("node:child_process", () => ({ execFile: mocks.execFile }));
vi.mock("./bundled-cli", () => ({ bundledCliPath: () => "C:\\Multica\\resources\\bin\\multica.exe" }));
import { checkWindowsUpdateInstall } from "./update-install-guard";

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllEnvs(); mocks.execFile.mockReset(); });

describe("Windows installer process guard", () => {
  it.each([
    [null, "clear\r\n", { allowed: true }],
    [null, "blocked\r\n", { allowed: false, reason: "runtime_running" }],
    [null, "unexpected", { allowed: false, reason: "probe_failed", diagnostic: "invalid_output" }],
    [new Error("private path / credential-like stderr"), "clear", { allowed: false, reason: "probe_failed", diagnostic: "launch_failed" }],
    [Object.assign(new Error("private timeout"), { killed: true }), "", { allowed: false, reason: "probe_failed", diagnostic: "timed_out" }],
  ])("fails closed on busy, unknown, or failed probes", async (error, output, expected) => {
    vi.stubEnv("SystemRoot", "C:\\Windows");
    mocks.execFile.mockImplementation((_exe, _args, _opts, callback) => callback(error, output));
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    await expect(checkWindowsUpdateInstall("win32")).resolves.toEqual(expected);
    expect(warn.mock.calls.flat().join(" ")).not.toMatch(/private|credential|stderr/);
    const [exe, args, options] = mocks.execFile.mock.calls[0]!;
    expect(exe).toContain("WindowsPowerShell");
    expect(args).toContain("-NonInteractive");
    expect(args.at(-1)).toContain("Get-CimInstance Win32_Process");
    expect(args.at(-1)).not.toMatch(/Stop-Process|Invoke-Expression|taskkill/i);
    expect(options).toMatchObject({ windowsHide: true, timeout: 6_000, env: { MULTICA_UPDATE_CLI_PATH: "C:\\Multica\\resources\\bin\\multica.exe" } });
  });

  it.each([undefined, "Windows"])("fails closed when SystemRoot is missing or relative", async (systemRoot) => {
    vi.stubEnv("SystemRoot", systemRoot);
    vi.spyOn(console, "warn").mockImplementation(() => {});
    expect(await checkWindowsUpdateInstall("win32")).toMatchObject({ allowed: false, diagnostic: "system_root_missing" });
    expect(mocks.execFile).not.toHaveBeenCalled();
  });

  it.each(["darwin", "linux"] as const)("does not query processes on %s", async (platform) => {
    expect(await checkWindowsUpdateInstall(platform)).toEqual({ allowed: true });
    expect(mocks.execFile).not.toHaveBeenCalled();
  });
});
