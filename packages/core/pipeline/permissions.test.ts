// @vitest-environment node
import { describe, expect, it } from "vitest";
import { canManageInitiatives, initiativeActions, type InitiativeActionContext } from "./permissions";

describe("canManageInitiatives", () => {
  it("allows owners and admins", () => {
    expect(canManageInitiatives("owner")).toBe(true);
    expect(canManageInitiatives("admin")).toBe(true);
  });

  it("refuses members and viewers, and anyone without a membership", () => {
    expect(canManageInitiatives("member")).toBe(false);
    expect(canManageInitiatives(null)).toBe(false);
    expect(canManageInitiatives(undefined)).toBe(false);
  });
});

const member: InitiativeActionContext = {
  admin: false,
  squadMember: false,
  exitMode: "attest",
  status: "active",
  hasPreviousStage: true,
};
const admin: InitiativeActionContext = { ...member, admin: true };
const owningMember: InitiativeActionContext = { ...member, squadMember: true };

function offered(context: Partial<InitiativeActionContext>) {
  return Object.entries(initiativeActions({ ...member, ...context }))
    .filter(([, shown]) => shown)
    .map(([action]) => action);
}

describe("initiativeActions", () => {
  it("offers nothing to a member outside the owning squad", () => {
    expect(offered(member)).toEqual([]);
  });

  it("offers Advance and Attest to a member of the owning squad, and everything to an admin", () => {
    expect(offered(owningMember)).toEqual(["advance", "attest"]);
    expect(offered(admin)).toEqual(["advance", "attest", "send_back", "shelve", "close"]);
  });

  it("keeps Send back, Shelve and Close from an owning squad member who is not an admin", () => {
    expect(offered({ ...owningMember, exitMode: "auto" })).toEqual(["advance"]);
  });

  it("withholds Advance on a gate stage, because the gate proposal decides", () => {
    expect(offered({ ...admin, exitMode: "gate" })).toEqual(["send_back", "shelve", "close"]);
    expect(offered({ ...owningMember, exitMode: "gate" })).toEqual([]);
  });

  it("offers Advance but not Attest on an auto stage, for an owning squad member", () => {
    expect(offered({ ...owningMember, exitMode: "auto" })).toEqual(["advance"]);
  });

  it("offers neither Advance nor Attest for an exit mode this client does not know", () => {
    expect(offered({ ...admin, exitMode: "unknown" })).toEqual(["send_back", "shelve", "close"]);
  });

  it("offers neither Advance nor Attest when the stage is not in the pipeline", () => {
    expect(offered({ ...admin, exitMode: null })).toEqual(["send_back", "shelve", "close"]);
  });

  it("offers Send back only when there is an earlier stage", () => {
    expect(offered({ ...admin, hasPreviousStage: false })).toEqual(["advance", "attest", "shelve", "close"]);
  });

  it("offers Shelve and Close on an operating initiative, but not Resume", () => {
    expect(offered({ ...admin, status: "operating", exitMode: "gate" })).toEqual(["send_back", "shelve", "close"]);
  });

  it("offers only Resume to an admin on a shelved initiative", () => {
    expect(offered({ ...admin, status: "shelved" })).toEqual(["resume"]);
    expect(offered({ ...owningMember, status: "shelved" })).toEqual([]);
  });

  it("offers nothing on a closed or unknown-status initiative", () => {
    expect(offered({ ...admin, status: "closed" })).toEqual([]);
    expect(offered({ ...owningMember, status: "unknown" })).toEqual([]);
    expect(offered({ ...admin, status: "unknown" })).toEqual([]);
  });
});
