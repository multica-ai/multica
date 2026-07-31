/**
 * The kill switch has to be safe in the direction that matters: an ambiguous
 * or missing signal must leave hang stack capture OFF, because "on" means the
 * main process holds a debugger channel open against every renderer.
 */
import { describe, expect, it } from "vitest";

import {
  DIAGNOSTICS_CONTROL_OFF,
  parseDiagnosticsControl,
} from "./diagnostics-control";

describe("parseDiagnosticsControl", () => {
  it("enables capture only on an explicit true", () => {
    expect(parseDiagnosticsControl({ stackCaptureEnabled: true })).toEqual({
      stackCaptureEnabled: true,
    });
  });

  it("stays off for an explicit false", () => {
    expect(parseDiagnosticsControl({ stackCaptureEnabled: false })).toEqual(
      DIAGNOSTICS_CONTROL_OFF,
    );
  });

  it.each([
    ["missing field", {}],
    ["a truthy non-boolean", { stackCaptureEnabled: "true" }],
    ["a number", { stackCaptureEnabled: 1 }],
    ["null", null],
    ["undefined", undefined],
    ["a string payload", "stackCaptureEnabled"],
    ["an array", []],
  ])("stays off for %s", (_label, payload) => {
    expect(parseDiagnosticsControl(payload)).toEqual(DIAGNOSTICS_CONTROL_OFF);
  });

  it("ignores unknown keys rather than inheriting them", () => {
    expect(
      parseDiagnosticsControl({ stackCaptureEnabled: true, somethingElse: true }),
    ).toEqual({ stackCaptureEnabled: true });
  });

  it("defaults to off", () => {
    expect(DIAGNOSTICS_CONTROL_OFF.stackCaptureEnabled).toBe(false);
  });
});
