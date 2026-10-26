// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { configureLinuxFileDialogs } from "./linux-file-dialog";

describe("configureLinuxFileDialogs", () => {
  it("requires a directory-capable portal on Linux", () => {
    const commandLine = { hasSwitch: vi.fn(() => false), appendSwitch: vi.fn() };
    configureLinuxFileDialogs(commandLine, "linux");
    expect(commandLine.appendSwitch).toHaveBeenCalledExactlyOnceWith(
      "xdg-portal-required-version", "3",
    );
  });

  it("preserves an explicit portal override", () => {
    const commandLine = { hasSwitch: vi.fn(() => true), appendSwitch: vi.fn() };
    configureLinuxFileDialogs(commandLine, "linux");
    expect(commandLine.hasSwitch).toHaveBeenCalledWith("xdg-portal-required-version");
    expect(commandLine.appendSwitch).not.toHaveBeenCalled();
  });

  it.each(["darwin", "win32"] as const)("leaves %s dialogs unchanged", (platform) => {
    const commandLine = { hasSwitch: vi.fn(), appendSwitch: vi.fn() };
    configureLinuxFileDialogs(commandLine, platform);
    expect(commandLine.hasSwitch).not.toHaveBeenCalled();
    expect(commandLine.appendSwitch).not.toHaveBeenCalled();
  });
});
