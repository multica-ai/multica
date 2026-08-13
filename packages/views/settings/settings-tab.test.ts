import { describe, expect, it } from "vitest";
import {
  DEFAULT_SETTINGS_TAB,
  resolveSettingsTab,
} from "./settings-tab";

const VALID = [
  "profile",
  "preferences",
  "integrations",
  "workspace",
  "repositories",
] as const;

describe("resolveSettingsTab", () => {
  it("defaults when the query is missing", () => {
    expect(resolveSettingsTab(null, VALID)).toBe(DEFAULT_SETTINGS_TAB);
    expect(resolveSettingsTab(undefined, VALID)).toBe(DEFAULT_SETTINGS_TAB);
  });

  it("maps the legacy lark alias to integrations", () => {
    expect(resolveSettingsTab("lark", VALID)).toBe("integrations");
  });

  it("keeps known tabs unchanged", () => {
    expect(resolveSettingsTab("integrations", VALID)).toBe("integrations");
    expect(resolveSettingsTab("profile", VALID)).toBe("profile");
  });

  it("falls back to the default for unknown values", () => {
    expect(resolveSettingsTab("not-a-tab", VALID)).toBe(DEFAULT_SETTINGS_TAB);
    expect(resolveSettingsTab("<script>", VALID)).toBe(DEFAULT_SETTINGS_TAB);
  });
});
