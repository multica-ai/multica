import { describe, expect, it } from "vitest";
import {
  AUTO_LABEL_NEW_ISSUES_SETTING,
  isAutoLabelNewIssuesEnabled,
  withAutoLabelNewIssuesSetting,
} from "./settings";

describe("workspace settings", () => {
  it("reads auto-label new issues only when explicitly enabled", () => {
    expect(isAutoLabelNewIssuesEnabled(undefined)).toBe(false);
    expect(isAutoLabelNewIssuesEnabled({ [AUTO_LABEL_NEW_ISSUES_SETTING]: false })).toBe(false);
    expect(isAutoLabelNewIssuesEnabled({ [AUTO_LABEL_NEW_ISSUES_SETTING]: "true" })).toBe(false);
    expect(isAutoLabelNewIssuesEnabled({ [AUTO_LABEL_NEW_ISSUES_SETTING]: true })).toBe(true);
  });

  it("merges auto-label setting without dropping existing workspace settings", () => {
    expect(withAutoLabelNewIssuesSetting({ github_enabled: true }, true)).toEqual({
      github_enabled: true,
      [AUTO_LABEL_NEW_ISSUES_SETTING]: true,
    });
  });
});
