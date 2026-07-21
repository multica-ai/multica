import { describe, it, expect } from "vitest";
import { daemonStatusAlive } from "./daemon-types";

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
