// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { IssueProperty } from "@multica/core/types";
import { newIssueFormProperties } from "./new-issue-properties";

const property = (id: string, overrides: Partial<IssueProperty>): IssueProperty => ({
  id, workspace_id: "ws", name: id, type: "select", config: {}, position: 0, archived: false,
  created_at: "", updated_at: "", ...overrides,
});

describe("newIssueFormProperties", () => {
  it("keeps active single-select properties that are required or always visible", () => {
    const shown = newIssueFormProperties([
      property("required", { required: true }),
      property("pinned", { always_visible: true }),
      property("plain", {}),
      property("archived", { required: true, archived: true }),
      property("pinned-text", { type: "text", always_visible: true }),
    ]);
    expect(shown.map((p) => p.id)).toEqual(["required", "pinned"]);
  });
});
