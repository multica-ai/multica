import { describe, expect, it } from "vitest";
import { sanitizeDiagnosticsControl } from "./diagnostics-control";

describe("sanitizeDiagnosticsControl", () => {
  it("accepts a well-formed control", () => {
    expect(
      sanitizeDiagnosticsControl({ cpuProfileEnabled: true, optOut: false }),
    ).toEqual({ cpuProfileEnabled: true, optOut: false });
  });

  it("copies only the two known fields, dropping extras", () => {
    expect(
      sanitizeDiagnosticsControl({
        cpuProfileEnabled: false,
        optOut: true,
        sneaky: "ignored",
      }),
    ).toEqual({ cpuProfileEnabled: false, optOut: true });
  });

  it.each([
    ["null", null],
    ["a primitive", 1],
    ["a missing flag", { optOut: false }],
    ["a missing optOut", { cpuProfileEnabled: true }],
    ["a non-boolean flag", { cpuProfileEnabled: "yes", optOut: false }],
    ["a non-boolean optOut", { cpuProfileEnabled: true, optOut: 0 }],
  ])("rejects %s so a malformed push can't flip the gate open", (_label, value) => {
    expect(sanitizeDiagnosticsControl(value)).toBeNull();
  });
});
