import { describe, expect, it } from "vitest";
import type { InboxItem, TimelineEntry } from "@multica/core/types";
import {
  deduplicateInboxItems,
  getAutopilotQuotaBody,
  getInboxDisplayTitle,
  getInboxNavigationTarget,
  getInboxNoticeBody,
  resolveTimelineHighlight,
} from "./inbox-display";

function item(overrides: Partial<InboxItem>): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "workspace-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue title",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-06-15T08:00:00Z",
    details: null,
    ...overrides,
  };
}

// The grouping and anchor rule itself is tested in packages/core/inbox/queries.test.ts.
describe("deduplicateInboxItems", () => {
  it("anchors an app request row like any other row, since mobile renders no requests", () => {
    const [row] = deduplicateInboxItems([
      item({ id: "comment", created_at: "2026-06-15T08:00:00Z", details: { comment_id: "comment-1" } }),
      item({ id: "app", type: "app_action_requested", created_at: "2026-06-15T08:02:00Z", details: { request_id: "request-1" } }),
    ]);
    expect(row?.id).toBe("app");
    expect(getInboxNavigationTarget(row!, "acme", "tap")?.params).toMatchObject({ id: "issue-1", highlight: "comment-1" });
  });

  it("gives an app request row no target when nothing unread has an entry", () => {
    const [row] = deduplicateInboxItems([
      item({ id: "comment", read: true, created_at: "2026-06-15T08:00:00Z", details: { comment_id: "comment-1" } }),
      item({ id: "app", type: "app_action_requested", created_at: "2026-06-15T08:02:00Z", details: { request_id: "request-1" } }),
    ]);
    expect(getInboxNavigationTarget(row!, "acme", "tap")?.params).toMatchObject({ id: "issue-1", highlight: undefined });
  });

  it("navigates a failed run's notification with its task id", () => {
    const [row] = deduplicateInboxItems([
      item({ id: "failed", type: "task_failed", created_at: "2026-06-15T08:02:00Z", details: { task_id: "task-1" } }),
    ]);
    expect(getInboxNavigationTarget(row!, "acme", "tap")?.params).toMatchObject({ highlight: "task-1" });
  });
});

describe("resolveTimelineHighlight", () => {
  const comment = (overrides: Partial<TimelineEntry>): TimelineEntry => ({
    type: "comment", id: "comment-1", actor_type: "member", actor_id: "user-1", content: "",
    parent_id: null, created_at: "2026-06-15T08:00:00Z", updated_at: "2026-06-15T08:00:00Z", ...overrides,
  });

  it("keeps an id the timeline has", () => {
    expect(resolveTimelineHighlight([comment({ id: "comment-1" })], "comment-1")).toBe("comment-1");
  });

  it("lands a task id on the failure notice its run posted", () => {
    const entries = [
      comment({ id: "agent-reply", actor_type: "agent", source_task_id: "task-1", comment_type: "comment" }),
      comment({ id: "notice", actor_type: "agent", source_task_id: "task-1", comment_type: "system" }),
    ];
    expect(resolveTimelineHighlight(entries, "task-1")).toBe("notice");
  });

  it("leaves a task id with nothing posted unmatched", () => {
    expect(resolveTimelineHighlight([comment({ id: "comment-1" })], "task-1")).toBeNull();
  });
});

describe("getInboxDisplayTitle", () => {
  it("uses the same stable quota title as web instead of backend fallback copy", () => {
    expect(
      getInboxDisplayTitle(
        item({
          issue_id: null,
          type: "autopilot_quota_exceeded",
          title: "Autopilot quota exceeded (100/100)",
        }),
      ),
    ).toBe("Autopilot run limit reached");
  });

  it("keeps paused autopilot copy on the server fallback path", () => {
    expect(
      getInboxDisplayTitle(
        item({
          issue_id: null,
          type: "autopilot_paused",
          title: "Paused after repeated failures",
        }),
      ),
    ).toBe("Paused after repeated failures");
  });
});

describe("getInboxNavigationTarget", () => {
  it("opens issue-less quota notices in their workspace sheet", () => {
    expect(
      getInboxNavigationTarget(
        item({ issue_id: null, type: "autopilot_quota_exceeded" }),
        "acme",
        "history-1",
      ),
    ).toEqual({
      pathname: "/[workspace]/inbox/[id]",
      params: { workspace: "acme", id: "inbox-1" },
    });
  });

  it("preserves issue navigation and opens paused notices in the same sheet", () => {
    expect(
      getInboxNavigationTarget(item({}), "acme", "history-1"),
    ).toMatchObject({
      pathname: "/[workspace]/issue/[id]",
      params: { workspace: "acme", id: "issue-1", h: "history-1" },
    });
    expect(
      getInboxNavigationTarget(
        item({ issue_id: null, type: "autopilot_paused" }),
        "acme",
        "history-1",
      ),
    ).toEqual({
      pathname: "/[workspace]/inbox/[id]",
      params: { workspace: "acme", id: "inbox-1" },
    });
  });
});

describe("getAutopilotQuotaBody", () => {
  it("formats the machine reset timestamp for the device locale", () => {
    const body = getAutopilotQuotaBody(
      item({
        issue_id: null,
        type: "autopilot_quota_exceeded",
        body: "Raw fallback 2026-09-01T00:00:00Z",
        details: {
          limit: "100",
          reset_at: "2026-09-01T00:00:00Z",
          autopilot_title: "Daily triage",
        },
      }),
    );

    expect(body).toContain("Daily triage");
    expect(body).toContain("limit of 100 runs");
    expect(body).not.toContain("2026-09-01T00:00:00Z");
  });

  it("keeps the server fallback when structured facts are incomplete", () => {
    expect(
      getAutopilotQuotaBody(
        item({
          issue_id: null,
          type: "autopilot_quota_exceeded",
          body: "Readable server fallback",
          details: { limit: "100" },
        }),
      ),
    ).toBe("Readable server fallback");
  });
});

describe("getInboxNoticeBody", () => {
  const joined = (details: InboxItem["details"]) =>
    item({
      issue_id: null,
      type: "project_owner_joined",
      title: "Secret Plan",
      body: "Server fallback",
      details,
    });

  it("names the workspace owner who joined a private project", () => {
    const notice = joined({ project_id: "p1", joined_by: "Grace" });

    expect(getInboxNoticeBody(notice)).toContain("Grace is a workspace owner");
    expect(getInboxDisplayTitle(notice)).toBe("Secret Plan");
    expect(getInboxNavigationTarget(notice, "acme", "history-1")).toEqual({
      pathname: "/[workspace]/inbox/[id]",
      params: { workspace: "acme", id: "inbox-1" },
    });
  });

  it("keeps the server body when the name is missing, and other notices as they were", () => {
    expect(getInboxNoticeBody(joined(null))).toBe("Server fallback");
    expect(
      getInboxNoticeBody(
        item({ issue_id: null, type: "autopilot_paused", body: "Paused" }),
      ),
    ).toBe("Paused");
  });
});
