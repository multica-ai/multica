// @vitest-environment node
import { describe, expect, it } from "vitest";
import { keyboardAvoidingEnabled } from "./keyboard-avoiding";

describe("keyboardAvoidingEnabled", () => {
  it("lets Android resize handle full-screen pages", () => {
    expect(keyboardAvoidingEnabled("fullScreen", "android")).toBe(false);
    expect(keyboardAvoidingEnabled("fullScreen", "ios")).toBe(true);
  });

  it("keeps padding on modal pages on both platforms", () => {
    expect(keyboardAvoidingEnabled("modal", "android")).toBe(true);
    expect(keyboardAvoidingEnabled("modal", "ios")).toBe(true);
  });
});
