// @vitest-environment node
import { describe, expect, it } from "vitest";
import { ProjectSchema, ProjectSourceChoiceErrorSchema } from "./schemas";

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

describe("execution source response compatibility", () => {
  it.each([undefined, "future_source", null, 42])("keeps the project when source metadata is unknown: %j", (intent) => {
    const parsed = ProjectSchema.parse({ ...project, source_intent: intent });
    expect(parsed.id).toBe(project.id);
    expect(parsed.source_intent).toBeUndefined();
  });
  it("preserves an explicit no-code choice", () => {
    expect(ProjectSchema.parse({ ...project, source_intent: "no_code" }).source_intent).toBe("no_code");
  });
});

it("validates a source-choice error before exposing project data to the UI", () => {
  expect(ProjectSourceChoiceErrorSchema.safeParse({code:"project_source_required",project:{id:"malformed"}}).success).toBe(false);
  const parsed=ProjectSourceChoiceErrorSchema.parse({code:"project_source_required",project:{...project,source_intent:"unspecified"}});
  expect(parsed.project.id).toBe(project.id);
});
