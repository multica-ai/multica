import { describe, expect, it, vi, beforeEach } from "vitest";

vi.mock("electron", () => ({
  shell: { openExternal: vi.fn().mockResolvedValue(undefined) },
}));

import { shell } from "electron";
import { isSafeExternalHttpUrl, openExternalSafely } from "./external-url";

describe("isSafeExternalHttpUrl", () => {
  it("allows http and https URLs", () => {
    expect(isSafeExternalHttpUrl("https://multica.ai")).toBe(true);
    expect(isSafeExternalHttpUrl("http://localhost:3000/auth")).toBe(true);
  });

  it("allows https URLs with embedded credentials", () => {
    // WHATWG URL parses these as https; OS-level handling is the shell's concern.
    expect(isSafeExternalHttpUrl("https://user:pass@example.com")).toBe(true);
  });

  it("normalizes scheme casing so uppercase variants can't bypass", () => {
    expect(isSafeExternalHttpUrl("HTTPS://example.com")).toBe(true);
    expect(isSafeExternalHttpUrl("FILE:///etc/passwd")).toBe(false);
  });

  it("rejects dangerous pseudo-schemes", () => {
    expect(isSafeExternalHttpUrl("javascript:alert(1)")).toBe(false);
    expect(
      isSafeExternalHttpUrl("data:text/html,<script>alert(1)</script>"),
    ).toBe(false);
  });

  it("rejects filesystem and network transport schemes", () => {
    expect(isSafeExternalHttpUrl("file:///etc/passwd")).toBe(false);
    expect(isSafeExternalHttpUrl("ftp://example.com/x")).toBe(false);
    expect(isSafeExternalHttpUrl("smb://share/x")).toBe(false);
  });

  it("rejects local-handler schemes used in past RCE chains", () => {
    expect(isSafeExternalHttpUrl("vscode://file/test")).toBe(false);
    expect(isSafeExternalHttpUrl("ms-msdt:/id%20PCWDiagnostic")).toBe(false);
  });

  it("rejects mailto and other non-web schemes", () => {
    expect(isSafeExternalHttpUrl("mailto:test@example.com")).toBe(false);
    expect(isSafeExternalHttpUrl("tel:+15551234567")).toBe(false);
  });

  it("rejects empty, whitespace, and malformed input", () => {
    expect(isSafeExternalHttpUrl("")).toBe(false);
    expect(isSafeExternalHttpUrl(" ")).toBe(false);
    expect(isSafeExternalHttpUrl("not a url")).toBe(false);
    expect(isSafeExternalHttpUrl("http://")).toBe(false);
  });
});

describe("openExternalSafely", () => {
  beforeEach(() => {
    vi.mocked(shell.openExternal).mockClear();
  });

  it("forwards http/https URLs to shell.openExternal", () => {
    openExternalSafely("https://multica.ai");
    expect(shell.openExternal).toHaveBeenCalledWith("https://multica.ai");
  });

  it("does not call shell.openExternal for rejected schemes", () => {
    openExternalSafely("file:///etc/passwd");
    openExternalSafely("javascript:alert(1)");
    openExternalSafely("not a url");
    expect(shell.openExternal).not.toHaveBeenCalled();
  });
});
