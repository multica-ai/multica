import { describe, it, expect } from "vitest";
import { buildIssueMarkdown } from "./build-issue-markdown";
import type { Issue, TimelineEntry, Label } from "@multica/core/types";

const baseIssue: Issue = {
  id: "issue-1",
  workspace_id: "ws-1",
  number: 5157,
  identifier: "AND-5157",
  title: "Reimplement export",
  description: "Body text",
  status: "todo",
  priority: "high",
  assignee_type: "agent",
  assignee_id: "agent-1",
  creator_type: "member",
  creator_id: "member-1",
  parent_issue_id: null,
  project_id: "proj-1",
  position: 0,
  start_date: null,
  due_date: null,
  metadata: {},
  created_at: "2026-05-28T09:34:03Z",
  updated_at: "2026-05-28T09:35:00Z",
};

const getActorName = (type: string, id: string) => {
  if (type === "agent" && id === "agent-1") return "frontend";
  if (type === "member" && id === "member-1") return "Alice";
  if (type === "member" && id === "member-2") return "Bob";
  return "Unknown";
};

describe("buildIssueMarkdown", () => {
  it("renders header, metadata, description and no-comments state", () => {
    const md = buildIssueMarkdown({
      issue: baseIssue,
      timeline: [],
      projectName: "Multica Contributions",
      getActorName,
    });
    expect(md).toContain("# AND-5157: Reimplement export");
    expect(md).toContain("- **Status:** todo");
    expect(md).toContain("- **Priority:** high");
    expect(md).toContain("- **Assignee:** frontend (agent)");
    expect(md).toContain("- **Creator:** Alice (member)");
    expect(md).toContain("- **Project:** Multica Contributions");
    expect(md).toContain("- **Created:** 2026-05-28T09:34:03Z");
    expect(md).toContain("## Description");
    expect(md).toContain("Body text");
    expect(md).toContain("## Comments");
    expect(md).toContain("_(no comments)_");
  });

  it("renders comments in chronological order with author + attachments", () => {
    const timeline: TimelineEntry[] = [
      {
        type: "comment",
        id: "c2",
        actor_type: "member",
        actor_id: "member-2",
        created_at: "2026-05-28T10:00:00Z",
        content: "second",
        attachments: [
          {
            id: "att-1",
            workspace_id: "ws-1",
            issue_id: null,
            comment_id: "c2",
            chat_session_id: null,
            chat_message_id: null,
            uploader_type: "member",
            uploader_id: "member-2",
            filename: "log.txt",
            url: "u",
            download_url: "d",
            content_type: "text/plain",
            size_bytes: 1,
            created_at: "2026-05-28T10:00:00Z",
          },
        ],
      },
      {
        type: "activity",
        id: "a1",
        actor_type: "agent",
        actor_id: "agent-1",
        created_at: "2026-05-28T09:50:00Z",
        action: "status_changed",
      },
      {
        type: "comment",
        id: "c1",
        actor_type: "agent",
        actor_id: "agent-1",
        created_at: "2026-05-28T09:45:00Z",
        content: "first",
      },
    ];

    const md = buildIssueMarkdown({
      issue: baseIssue,
      timeline,
      getActorName,
    });

    const firstIdx = md.indexOf("first");
    const secondIdx = md.indexOf("second");
    expect(firstIdx).toBeGreaterThan(-1);
    expect(secondIdx).toBeGreaterThan(firstIdx);

    expect(md).toContain("### frontend (agent) — 2026-05-28T09:45:00Z");
    expect(md).toContain("### Bob (member) — 2026-05-28T10:00:00Z");
    expect(md).toContain("**Attachments:**");
    expect(md).toContain("- log.txt");
    expect(md).not.toContain("status_changed");
  });

  it("falls back when description is empty and labels are missing", () => {
    const md = buildIssueMarkdown({
      issue: { ...baseIssue, description: null },
      timeline: [],
      getActorName,
    });
    expect(md).toContain("- **Labels:** —");
    expect(md).toContain("_(no description)_");
  });

  it("renders explicit labels argument", () => {
    const labels: Label[] = [
      { id: "l1", workspace_id: "ws-1", name: "bug", color: "#ff0000", created_at: "", updated_at: "" },
      { id: "l2", workspace_id: "ws-1", name: "ui", color: "#00ff00", created_at: "", updated_at: "" },
    ];
    const md = buildIssueMarkdown({
      issue: baseIssue,
      timeline: [],
      labels,
      getActorName,
    });
    expect(md).toContain("- **Labels:** bug, ui");
  });

  it("handles missing assignee", () => {
    const md = buildIssueMarkdown({
      issue: { ...baseIssue, assignee_type: null, assignee_id: null },
      timeline: [],
      getActorName,
    });
    expect(md).toContain("- **Assignee:** —");
  });
});
