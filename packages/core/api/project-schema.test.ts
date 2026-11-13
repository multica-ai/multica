// @vitest-environment node
import { describe, expect, it } from "vitest";
import { ProjectSchema } from "./schemas";

const project = {
  id: "project", workspace_id: "workspace", title: "Project", description: null,
  icon: null, status: "planned", priority: "none", lead_type: null, lead_id: null,
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
};

describe("project creator response compatibility", () => {
  it.each([{}, { creator_type: null, creator_id: null }, { creator_type: 42, creator_id: {} }])("defaults missing or malformed metadata: %j", (creator) => {
    const parsed = ProjectSchema.parse({ ...project, ...creator });
    expect(parsed.id).toBe("project");
    expect(parsed.creator_type).toBeNull();
    expect(parsed.creator_id).toBeNull();
  });
  it("preserves creators and unknown future types without granting rights", () => {
    for (const type of ["member", "agent", "future"]) {
      const parsed = ProjectSchema.parse({ ...project, creator_type: type, creator_id: "creator" });
      expect(parsed.creator_type).toBe(type);
      expect(parsed.creator_id).toBe("creator");
    }
  });
});
