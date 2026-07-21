import { describe, it, expect } from "vitest";
import { DEFAULT_GITLAB_ISSUE_SYNC_LABEL, deriveGitLabSettings } from "./settings";
import type { Workspace } from "../types";

function ws(settings: Record<string, unknown>): Pick<Workspace, "settings"> {
  return { settings };
}

describe("deriveGitLabSettings", () => {
  it("defaults every flag to true when workspace is null", () => {
    expect(deriveGitLabSettings(null)).toEqual({
      enabled: true,
      mrSidebar: true,
      issueSync: true,
      commentSync: true,
      issueSyncLabel: DEFAULT_GITLAB_ISSUE_SYNC_LABEL,
    });
  });

  it("defaults every flag to true on empty settings", () => {
    expect(deriveGitLabSettings(ws({}))).toEqual({
      enabled: true,
      mrSidebar: true,
      issueSync: true,
      commentSync: true,
      issueSyncLabel: DEFAULT_GITLAB_ISSUE_SYNC_LABEL,
    });
  });

  it("master switch off forces every dependent flag off", () => {
    expect(
      deriveGitLabSettings(ws({ gitlab_enabled: false, gitlab_mr_sidebar_enabled: true, gitlab_issue_sync_enabled: true, gitlab_comment_sync_enabled: true })),
    ).toEqual({
      enabled: false,
      mrSidebar: false,
      issueSync: false,
      commentSync: false,
      issueSyncLabel: DEFAULT_GITLAB_ISSUE_SYNC_LABEL,
    });
  });

  it("each sub-flag can be flipped independently when master is on", () => {
    expect(deriveGitLabSettings(ws({ gitlab_mr_sidebar_enabled: false }))).toMatchObject({ enabled: true, mrSidebar: false, issueSync: true, commentSync: true });
    expect(deriveGitLabSettings(ws({ gitlab_issue_sync_enabled: false }))).toMatchObject({ enabled: true, mrSidebar: true, issueSync: false, commentSync: true });
    expect(deriveGitLabSettings(ws({ gitlab_comment_sync_enabled: false }))).toMatchObject({ enabled: true, mrSidebar: true, issueSync: true, commentSync: false });
  });

  it("reads a custom issue sync label and falls back for blank values", () => {
    expect(deriveGitLabSettings(ws({ gitlab_issue_sync_label: "multica" })).issueSyncLabel).toBe("multica");
    expect(deriveGitLabSettings(ws({ gitlab_issue_sync_label: "  triage  " })).issueSyncLabel).toBe("triage");
    expect(deriveGitLabSettings(ws({ gitlab_issue_sync_label: "" })).issueSyncLabel).toBe(DEFAULT_GITLAB_ISSUE_SYNC_LABEL);
    expect(deriveGitLabSettings(ws({ gitlab_issue_sync_label: "   " })).issueSyncLabel).toBe(DEFAULT_GITLAB_ISSUE_SYNC_LABEL);
    expect(deriveGitLabSettings(ws({ gitlab_issue_sync_label: 42 })).issueSyncLabel).toBe(DEFAULT_GITLAB_ISSUE_SYNC_LABEL);
  });
});
