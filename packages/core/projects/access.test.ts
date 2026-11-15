import { describe, expect, it } from "vitest";
import { canChangeProjectLead, canManageProjectAccess } from "./access";

const led = { visibility: "private" as const, lead_type: "member" as const, lead_id: "lead" };
const agentLed = { visibility: "private" as const, lead_type: "agent" as const, lead_id: "agent" };
const unled = { visibility: "private" as const, lead_type: null, lead_id: null };
const open = { visibility: "workspace" as const, lead_type: "member" as const, lead_id: "lead" };

describe("project access rules", () => {
  it("lets owners and admins manage access whoever the manager is", () => {
    expect(canManageProjectAccess(agentLed, "owner", "owner", "creator")).toBe(true);
    expect(canManageProjectAccess(agentLed, "admin", "admin", null)).toBe(true);
    expect(canManageProjectAccess(open, "admin", "admin", null)).toBe(true);
    expect(canManageProjectAccess(agentLed, null, "owner", "creator")).toBe(false);
  });

  it("lets the member the server names manage a private project", () => {
    expect(canManageProjectAccess(led, "lead", "member", "lead")).toBe(true);
    expect(canManageProjectAccess(led, "someone", "member", "lead")).toBe(false);
    // An agent lead or no lead: the creator, or whoever is next in line.
    expect(canManageProjectAccess(agentLed, "creator", "member", "creator")).toBe(true);
    expect(canManageProjectAccess(unled, "creator", "member", "creator")).toBe(true);
    expect(canManageProjectAccess(agentLed, "someone", "member", "creator")).toBe(false);
    // The server's answer wins over the lead, e.g. a lead who has left.
    expect(canManageProjectAccess(led, "lead", "member", "creator")).toBe(false);
  });

  it("falls back to a person who leads it while no manager is named", () => {
    expect(canManageProjectAccess(led, "lead", "member", undefined)).toBe(true);
    expect(canManageProjectAccess(led, "lead", "member", null)).toBe(true);
    expect(canManageProjectAccess(led, "someone", "member", undefined)).toBe(false);
    expect(canManageProjectAccess(agentLed, "agent", "member", undefined)).toBe(false);
    expect(canManageProjectAccess(unled, "creator", "member", undefined)).toBe(false);
  });

  it("gives a member nothing for leading a workspace-visible project", () => {
    expect(canManageProjectAccess(open, "lead", "member", "lead")).toBe(false);
  });

  it("keeps the lead of a private project to its access managers", () => {
    expect(canChangeProjectLead(open, "someone", "member", null)).toBe(true);
    expect(canChangeProjectLead(agentLed, "someone", "member", "creator")).toBe(false);
    expect(canChangeProjectLead(agentLed, "creator", "member", "creator")).toBe(true);
    expect(canChangeProjectLead(agentLed, "owner", "owner", "creator")).toBe(true);
  });
});
