import { describe, it, expect } from "vitest";
import { deriveGravatarSettings } from "./settings";

describe("deriveGravatarSettings", () => {
  it("defaults to disabled when workspace is null", () => {
    expect(deriveGravatarSettings(null).enabled).toBe(false);
  });

  it("defaults to disabled when settings is empty", () => {
    expect(deriveGravatarSettings({ settings: {} }).enabled).toBe(false);
  });

  it("is enabled only when gravatar_enabled is explicitly true", () => {
    expect(deriveGravatarSettings({ settings: { gravatar_enabled: true } }).enabled).toBe(true);
  });

  it("is disabled when gravatar_enabled is false", () => {
    expect(deriveGravatarSettings({ settings: { gravatar_enabled: false } }).enabled).toBe(false);
  });

  it("is disabled for truthy non-boolean values (strict check)", () => {
    expect(deriveGravatarSettings({ settings: { gravatar_enabled: "yes" } }).enabled).toBe(false);
    expect(deriveGravatarSettings({ settings: { gravatar_enabled: 1 } }).enabled).toBe(false);
  });
});
