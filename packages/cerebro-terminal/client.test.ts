import { describe, expect, it } from "vitest";
import { encodeStdin, decodeStdout } from "./client";

describe("terminal codec", () => {
  it("roundtrips ASCII through base64", () => {
    const frame = encodeStdin("hello\n");
    expect(frame.type).toBe("stdin");
    const decoded = new TextDecoder().decode(decodeStdout(frame.data));
    expect(decoded).toBe("hello\n");
  });

  it("preserves binary bytes (no UTF-8 mangling)", () => {
    const bytes = new Uint8Array([0x1b, 0x5b, 0x32, 0x4a]); // ANSI clear
    const frame = encodeStdin(bytes);
    const decoded = decodeStdout(frame.data);
    expect(Array.from(decoded)).toEqual(Array.from(bytes));
  });

  it("handles UTF-8 multibyte characters", () => {
    const frame = encodeStdin("ø🐙");
    const decoded = new TextDecoder().decode(decodeStdout(frame.data));
    expect(decoded).toBe("ø🐙");
  });
});
