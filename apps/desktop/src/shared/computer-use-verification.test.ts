// @vitest-environment node
import { describe, expect, it } from "vitest";
import { isVerificationApp } from "./computer-use-verification";

describe("isVerificationApp", () => {
  it("matches only verification environment bundle IDs", () => {
    expect(isVerificationApp("ai.multica.verification.0d2c9f3e-1111-4222-8333-444455556666")).toBe(true);
    expect(isVerificationApp("ai.multica.verification.")).toBe(false);
    expect(isVerificationApp("ai.multica.desktop")).toBe(false);
    expect(isVerificationApp("com.github.Electron")).toBe(false);
  });
});
