import { describe, expect, it } from "vitest";
import { canChangeProjectLead, canManageProjectAccess, isAllowedProjectLead } from "./access";

const privateProject = { visibility: "private" as const, lead_type: "member" as const, lead_id: "lead" };
const openProject = { visibility: "workspace" as const, lead_type: null, lead_id: null };

describe("project access rules", () => {
  it("lets owners, admins and the member lead manage a private project's access", () => {
    expect(canManageProjectAccess(privateProject, "owner", "owner")).toBe(true);
    expect(canManageProjectAccess(privateProject, "admin", "admin")).toBe(true);
    expect(canManageProjectAccess(privateProject, "lead", "member")).toBe(true);
    expect(canManageProjectAccess(privateProject, "someone", "member")).toBe(false);
    expect(canManageProjectAccess(privateProject, null, "owner")).toBe(false);
  });

  it("keeps the lead of a private project to its access managers", () => {
    expect(canChangeProjectLead(openProject, "someone", "member")).toBe(true);
    expect(canChangeProjectLead(privateProject, "someone", "member")).toBe(false);
    expect(canChangeProjectLead(privateProject, "lead", "member")).toBe(true);
  });

  it("accepts only a person as the lead of a private project", () => {
    expect(isAllowedProjectLead(privateProject, { lead_type: "member", lead_id: "m1" })).toBe(true);
    expect(isAllowedProjectLead(privateProject, { lead_type: "agent", lead_id: "a1" })).toBe(false);
    expect(isAllowedProjectLead(privateProject, { lead_type: null, lead_id: null })).toBe(false);
    expect(isAllowedProjectLead(privateProject, { lead_type: "member", lead_id: null })).toBe(false);
    // A workspace-visible project keeps every choice.
    expect(isAllowedProjectLead(openProject, { lead_type: "agent", lead_id: "a1" })).toBe(true);
    expect(isAllowedProjectLead(openProject, { lead_type: null, lead_id: null })).toBe(true);
  });
});
