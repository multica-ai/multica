// @vitest-environment node
import { describe, expect, it } from "vitest";
import { EMPTY_ORG_MAP, OrgMapSchema } from "./org-map-schema";

describe("OrgMapSchema", () => {
  it("parses an empty body to the empty map", () => {
    expect(OrgMapSchema.parse({})).toEqual(EMPTY_ORG_MAP);
  });

  it("fills defaults for optional fields and keeps the squad's leader and project", () => {
    const parsed = OrgMapSchema.parse({
      squads: [{ id: "s1", name: "Research", leader: { name: "Ada" }, project: { title: "Q3" } }],
    });
    expect(parsed.squads[0]).toMatchObject({
      id: "s1",
      floor_id: null,
      leader: { name: "Ada", runtime_status: "", archived: false },
      project: { title: "Q3", workflow_name: "" },
      open_issues: 0,
      members: [],
    });
  });

  it("refuses a squad with no id, so the caller falls back to the empty map", () => {
    expect(OrgMapSchema.safeParse({ squads: [{ name: "No id" }] }).success).toBe(false);
  });
});
