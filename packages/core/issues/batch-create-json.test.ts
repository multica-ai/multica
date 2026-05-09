import { describe, expect, it } from "vitest";
import {
  BATCH_ISSUE_TEMPLATE_FILENAME,
  createBatchIssueTemplateJSON,
  parseBatchCreateIssuesJSON,
} from "./batch-create-json";

describe("batch create issue JSON helpers", () => {
  it("produces the supported template file contents", () => {
    expect(BATCH_ISSUE_TEMPLATE_FILENAME).toBe("multica-batch-issues-template.json");
    const parsed = JSON.parse(createBatchIssueTemplateJSON());
    expect(Array.isArray(parsed.issues)).toBe(true);
    expect(parsed.issues[0]).toMatchObject({
      title: "Fix login empty state copy",
      status: "todo",
      assignee_type: "member",
    });
  });

  it("uses provided reference IDs in the supported template", () => {
    const parsed = JSON.parse(createBatchIssueTemplateJSON({
      memberAssigneeId: "user-1",
      agentAssigneeId: "agent-1",
      projectId: "project-1",
    }));

    expect(parsed.issues[0]).toMatchObject({
      assignee_type: "member",
      assignee_id: "user-1",
      project_id: "project-1",
    });
    expect(parsed.issues[1]).toMatchObject({
      assignee_type: "agent",
      assignee_id: "agent-1",
    });
  });

  it("parses and normalizes pasted JSON", () => {
    const result = parseBatchCreateIssuesJSON(`{
      "issues": [
        {
          "title": "  Ship import  ",
          "description": "Line one\\nLine two",
          "status": " ",
          "assignee_type": "agent",
          "assignee_id": "aaaaaaaa-1111-1111-1111-111111111111",
          "project_id": " bbbbbbbb-2222-2222-2222-222222222222 "
        }
      ]
    }`);

    expect(result.ok).toBe(true);
    if (!result.ok) throw new Error("expected parse success");
    expect(result.request.issues).toEqual([
      {
        title: "Ship import",
        description: "Line one\nLine two",
        assignee_type: "agent",
        assignee_id: "aaaaaaaa-1111-1111-1111-111111111111",
        project_id: "bbbbbbbb-2222-2222-2222-222222222222",
      },
    ]);
  });

  it("reports invalid JSON before API calls", () => {
    const result = parseBatchCreateIssuesJSON("{ nope");

    expect(result.ok).toBe(false);
    expect(result.errors).toContainEqual(
      expect.objectContaining({ row: 0, field: "json", code: "invalid_json" }),
    );
    expect(result.errors[0]?.message).toMatch(/^JSON could not be parsed: /);
  });

  it("reports unknown row fields as row-level validation errors", () => {
    const result = parseBatchCreateIssuesJSON(JSON.stringify({
      issues: [
        {
          title: "Do the import",
          priority: "high",
        },
      ],
    }));

    expect(result.ok).toBe(false);
    expect(result.errors).toContainEqual(
      expect.objectContaining({ row: 1, field: "priority", code: "unsupported_field" }),
    );
  });

  it("requires paired assignee fields", () => {
    const result = parseBatchCreateIssuesJSON(JSON.stringify({
      issues: [
        {
          title: "Needs an assignee type",
          assignee_id: "aaaaaaaa-1111-1111-1111-111111111111",
        },
      ],
    }));

    expect(result.ok).toBe(false);
    expect(result.errors).toContainEqual(
      expect.objectContaining({ row: 1, field: "assignee_type", code: "assignee_pair_required" }),
    );
  });
});
