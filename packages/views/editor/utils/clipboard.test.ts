import { afterEach, describe, it, expect, vi } from "vitest";
import { copyText } from "@multica/ui/lib/clipboard";

// jsdom implements neither navigator.clipboard nor document.execCommand, so we
// define them per test to simulate secure (https/localhost) vs insecure (plain
// http://) contexts.
function setClipboard(value: unknown): void {
  Object.defineProperty(navigator, "clipboard", {
    value,
    configurable: true,
    writable: true,
  });
}

function setExecCommand(result: boolean): ReturnType<typeof vi.fn> {
  const mock = vi.fn().mockReturnValue(result);
  Object.defineProperty(document, "execCommand", {
    value: mock,
    configurable: true,
    writable: true,
  });
  return mock;
}

afterEach(() => {
  setClipboard(undefined);
  vi.restoreAllMocks();
});

describe("copyText", () => {
  it("uses the async Clipboard API in a secure context and returns true", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    setClipboard({ writeText });

    const ok = await copyText("hello");

    expect(ok).toBe(true);
    expect(writeText).toHaveBeenCalledWith("hello");
  });

  it("falls back to execCommand when navigator.clipboard is unavailable (http://)", async () => {
    setClipboard(undefined);
    const execCommand = setExecCommand(true);

    const ok = await copyText("from-http");

    expect(ok).toBe(true);
    expect(execCommand).toHaveBeenCalledWith("copy");
    // The temporary textarea must be cleaned up afterwards.
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("falls back to execCommand when writeText rejects", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("blocked"));
    setClipboard({ writeText });
    const execCommand = setExecCommand(true);

    const ok = await copyText("retry");

    expect(ok).toBe(true);
    expect(writeText).toHaveBeenCalledWith("retry");
    expect(execCommand).toHaveBeenCalledWith("copy");
  });

  it("returns false when the fallback execCommand fails", async () => {
    setClipboard(undefined);
    setExecCommand(false);

    const ok = await copyText("nope");

    expect(ok).toBe(false);
  });
});
