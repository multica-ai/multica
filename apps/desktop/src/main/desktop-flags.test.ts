// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { applyDesktopFlags } from "./desktop-flags";
import { configureLinuxFileDialogs } from "./linux-file-dialog";

function nativeCommandLine(initial: Record<string, string | undefined> = {}) {
  const switches = new Map(Object.entries(initial).map(([name, value]) => [name.toLowerCase(), value]));
  return {
    switches,
    hasSwitch: (name: string) => switches.has(name.toLowerCase()),
    appendSwitch: vi.fn((name: string, value?: string) => { switches.set(name.toLowerCase(), value); }),
  };
}

describe("desktop flags file", () => {
  let fixtureHome: string;
  let configDir: string;
  const log = { info: vi.fn(), warn: vi.fn() };

  beforeEach(() => {
    fixtureHome = mkdtempSync(join(tmpdir(), "multica-desktop-flags-"));
    configDir = join(fixtureHome, ".config");
    mkdirSync(configDir);
    vi.clearAllMocks();
    vi.stubEnv("XDG_CONFIG_HOME", "");
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    rmSync(fixtureHome, { recursive: true, force: true });
  });

  function write(body: string | Buffer, directory = configDir) {
    const path = join(directory, "multica-flags.conf");
    writeFileSync(path, body);
    return path;
  }

  it("reads its home fallback before ready without changing application argv", () => {
    write("--enable-blink-features=MiddleClickAutoscroll\n--disable-gpu\n");
    const commandLine = nativeCommandLine();
    const originalArgv = [...process.argv];
    applyDesktopFlags(commandLine, { home: fixtureHome, log });
    expect(commandLine.switches.get("enable-blink-features")).toBe("MiddleClickAutoscroll");
    expect(commandLine.switches.has("disable-gpu")).toBe(true);
    expect(commandLine.appendSwitch).toHaveBeenCalledWith("disable-gpu");
    expect(process.argv).toEqual(originalArgv);
    expect(log.info).toHaveBeenCalled();
  });

  it("reads only the XDG file when a configuration directory is selected", () => {
    const selectedDir = join(fixtureHome, "selected");
    mkdirSync(selectedDir);
    write("--password-store=basic", selectedDir);
    write("--disable-gpu");
    vi.stubEnv("XDG_CONFIG_HOME", selectedDir);
    const commandLine = nativeCommandLine();
    applyDesktopFlags(commandLine, { home: fixtureHome, log });
    expect([...commandLine.switches]).toEqual([["password-store", "basic"]]);
  });

  it("does not fall back to the home file when the selected XDG file is missing", () => {
    write("--disable-gpu");
    const commandLine = nativeCommandLine();
    applyDesktopFlags(commandLine, { home: fixtureHome, configHome: join(fixtureHome, "missing"), log });
    expect(commandLine.switches.size).toBe(0);
    expect(log.info).not.toHaveBeenCalled();
    expect(log.warn).not.toHaveBeenCalled();
  });

  it("keeps real command-line values and valueless switches ahead of the file", () => {
    write("--enable-features=FromFile\n--disable-gpu=false\n--password-store=basic");
    const commandLine = nativeCommandLine({ "enable-features": "FromCLI", "disable-gpu": undefined });
    applyDesktopFlags(commandLine, { home: fixtureHome, log });
    expect(commandLine.switches.get("enable-features")).toBe("FromCLI");
    expect(commandLine.switches.get("disable-gpu")).toBeUndefined();
    expect(commandLine.hasSwitch("disable-gpu")).toBe(true);
    expect(commandLine.switches.get("password-store")).toBe("basic");
  });

  it("lets the file override the app's Linux portal default", () => {
    write("--xdg-portal-required-version=2");
    const commandLine = nativeCommandLine();
    applyDesktopFlags(commandLine, { home: fixtureHome, log });
    configureLinuxFileDialogs(commandLine, "linux");
    expect(commandLine.switches.get("xdg-portal-required-version")).toBe("2");
  });

  it("uses the last duplicate file entry, with case-insensitive switch names", () => {
    write("--password-store=first\n--PASSWORD-STORE=last");
    const commandLine = nativeCommandLine();
    applyDesktopFlags(commandLine, { home: fixtureHome, log });
    expect([...commandLine.switches]).toEqual([["password-store", "last"]]);
  });

  it("preserves literal spaced values, equals signs, hashes, quotes and shell syntax", () => {
    const marker = join(fixtureHome, "must-not-be-created");
    const literal = `MAP example.test 127.0.0.1 #literal=a $(touch ${marker}) $VARIABLE "quoted"  `;
    write(`\uFEFF# comment\r\n\r\n  --host-resolver-rules=${literal}\r\n--unknown-future-switch=value\r\n--empty=\r\n`);
    const commandLine = nativeCommandLine();
    applyDesktopFlags(commandLine, { home: fixtureHome, log });
    expect(commandLine.switches.get("host-resolver-rules")).toBe(literal);
    expect(commandLine.switches.get("unknown-future-switch")).toBe("value");
    expect(commandLine.switches.get("empty")).toBe("");
    expect(existsSync(marker)).toBe(false);
  });

  it("skips malformed and app-owned arguments but still applies later valid switches", () => {
    write("not-a-switch\nmultica://issues/123\n--multica-locale=ru\n--appimage-extract\n--\n---broken\n--two switches\n--bad=value\0suffix\n--disable-gpu");
    const commandLine = nativeCommandLine();
    applyDesktopFlags(commandLine, { home: fixtureHome, log });
    expect([...commandLine.switches]).toEqual([["disable-gpu", undefined]]);
    expect(log.warn).toHaveBeenCalled();
  });

  it("silently ignores a missing flags file", () => {
    applyDesktopFlags(nativeCommandLine(), { home: fixtureHome, log });
    expect(log.info).not.toHaveBeenCalled();
    expect(log.warn).not.toHaveBeenCalled();
  });

  it("warns without throwing when the flags path cannot be read as a file", () => {
    mkdirSync(join(configDir, "multica-flags.conf"));
    expect(() => applyDesktopFlags(nativeCommandLine(), { home: fixtureHome, log })).not.toThrow();
    expect(log.warn).toHaveBeenCalled();
  });

  it("continues after native append rejection without logging values or native errors", () => {
    const secret = "SYNTHETIC_PRIVATE_FLAG_VALUE";
    write(`--reject=${secret}\n--disable-gpu`);
    const commandLine = nativeCommandLine();
    commandLine.appendSwitch.mockImplementation((name, value) => {
      if (name === "reject") throw new Error(secret);
      commandLine.switches.set(name, value);
    });
    expect(() => applyDesktopFlags(commandLine, { home: fixtureHome, log })).not.toThrow();
    expect(commandLine.hasSwitch("disable-gpu")).toBe(true);
    expect(log.warn).toHaveBeenCalled();
    expect(JSON.stringify([log.info.mock.calls, log.warn.mock.calls])).not.toContain(secret);
  });

  it("keeps malformed file content and applied values out of diagnostics", () => {
    const secret = "SYNTHETIC_PRIVATE_FLAG_VALUE";
    write(`${secret}\n--user-agent=${secret}\n`);
    applyDesktopFlags(nativeCommandLine(), { home: fixtureHome, log });
    expect(log.info).toHaveBeenCalled();
    expect(log.warn).toHaveBeenCalled();
    expect(JSON.stringify([log.info.mock.calls, log.warn.mock.calls])).not.toContain(secret);
  });
});
