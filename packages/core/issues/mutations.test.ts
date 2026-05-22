import { beforeEach, describe, expect, it } from "vitest";
import { applyIssueCreatePreferences } from "./mutations";
import { useIssueCreatePreferencesStore } from "./stores/create-preferences-store";

describe("applyIssueCreatePreferences", () => {
  beforeEach(() => {
    useIssueCreatePreferencesStore.setState({ duplicatePolicy: "confirm" });
  });

  it("leaves create requests unchanged when duplicate confirmation is enabled", () => {
    const request = { title: "Ship the fix" };

    expect(applyIssueCreatePreferences(request)).toBe(request);
  });

  it("adds allow_duplicate when the local preference allows duplicate titles", () => {
    useIssueCreatePreferencesStore.setState({ duplicatePolicy: "allow" });

    expect(applyIssueCreatePreferences({ title: "Ship the fix" })).toEqual({
      title: "Ship the fix",
      allow_duplicate: true,
    });
  });

  it("does not override an explicit allow_duplicate value", () => {
    useIssueCreatePreferencesStore.setState({ duplicatePolicy: "allow" });
    const request = { title: "Ship the fix", allow_duplicate: false };

    expect(applyIssueCreatePreferences(request)).toBe(request);
  });
});
