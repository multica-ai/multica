// @vitest-environment node
import { describe, it, expect } from "vitest";
import {
  daemonStateDescription,
  daemonStatusAlive,
  normalizePreventSleepMode,
} from "./daemon-types";

describe("normalizePreventSleepMode", () => {
  it("keeps each supported mode", () => {
    expect(normalizePreventSleepMode("on")).toBe("on");
    expect(normalizePreventSleepMode("agent")).toBe("agent");
    expect(normalizePreventSleepMode("off")).toBe("off");
  });

  it("falls back to off for a missing or unknown value", () => {
    expect(normalizePreventSleepMode(undefined)).toBe("off");
    expect(normalizePreventSleepMode(true)).toBe("off");
    expect(normalizePreventSleepMode("always")).toBe("off");
  });
});

describe("daemonStatusAlive", () => {
  it("treats a ready daemon as alive", () => {
    expect(daemonStatusAlive("running")).toBe(true);
  });

  it("treats a still-booting daemon as alive", () => {
    // /health binds before preflight and reports "starting" until ready; the
    // Desktop must not spawn a second daemon over it (the CLI rejects that as
    // "already running").
    expect(daemonStatusAlive("starting")).toBe(true);
  });

  it("treats stopped / unknown / missing as not alive", () => {
    expect(daemonStatusAlive("stopped")).toBe(false);
    expect(daemonStatusAlive("bogus")).toBe(false);
    expect(daemonStatusAlive("")).toBe(false);
    expect(daemonStatusAlive(undefined)).toBe(false);
  });
});

describe("daemonStateDescription", () => {
  it("makes an exhausted recovery budget actionable", () => {
    expect(daemonStateDescription("recovery_paused", 0)).toMatch(
      /start manually/i,
    );
  });
});
