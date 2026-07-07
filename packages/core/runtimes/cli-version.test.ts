import { describe, it, expect } from "vitest";
import {
  checkQuickCreateCliVersion,
  handoffSupported,
  MIN_HANDOFF_CLI_VERSION,
} from "./cli-version";

describe("checkQuickCreateCliVersion", () => {
  it("returns ok for a tagged release at or above the minimum", () => {
    expect(checkQuickCreateCliVersion("v0.2.21").state).toBe("ok");
    expect(checkQuickCreateCliVersion("0.3.1").state).toBe("ok");
  });

  it("returns too_old for a tagged release below the minimum", () => {
    expect(checkQuickCreateCliVersion("v0.2.20").state).toBe("too_old");
    expect(checkQuickCreateCliVersion("v0.2.15").state).toBe("too_old");
  });

  it("returns missing for empty or unparsable input", () => {
    expect(checkQuickCreateCliVersion("").state).toBe("missing");
    expect(checkQuickCreateCliVersion(undefined).state).toBe("missing");
    expect(checkQuickCreateCliVersion("not-a-version").state).toBe("missing");
  });

  it("treats git-describe dev builds as ok regardless of base tag", () => {
    expect(checkQuickCreateCliVersion("v0.2.15-235-gdaf0e935").state).toBe("ok");
    expect(checkQuickCreateCliVersion("v0.2.15-235-gdaf0e935-dirty").state).toBe("ok");
    expect(checkQuickCreateCliVersion("0.1.0-1-gabc1234").state).toBe("ok");
  });
});

// Mirrors server/pkg/agent/handoff_version_test.go so the frontend soft-gate
// signal and the server's authoritative one agree by construction.
describe("handoffSupported", () => {
  it("supports a tagged release at or above the minimum", () => {
    expect(handoffSupported(MIN_HANDOFF_CLI_VERSION)).toBe(true);
    expect(handoffSupported("0.4.0")).toBe(true);
    expect(handoffSupported("v0.3.28")).toBe(true);
  });

  it("does not support a tagged release below the minimum", () => {
    expect(handoffSupported("0.3.26")).toBe(false);
    expect(handoffSupported("0.2.21")).toBe(false);
  });

  it("fails closed on empty or unparsable input", () => {
    expect(handoffSupported("")).toBe(false);
    expect(handoffSupported(undefined)).toBe(false);
    expect(handoffSupported(null)).toBe(false);
    expect(handoffSupported("garbage")).toBe(false);
  });

  it("treats git-describe dev builds as supported regardless of base tag", () => {
    expect(handoffSupported("v0.3.0-5-gabc1234")).toBe(true);
    expect(handoffSupported("v0.1.0-235-gdaf0e935-dirty")).toBe(true);
  });
});
