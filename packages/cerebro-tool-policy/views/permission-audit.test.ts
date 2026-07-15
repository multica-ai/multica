import { describe, expect, it } from "vitest";

import {
  buildPermissionAuditContexts,
  buildPermissionAuditRows,
} from "./permission-audit";

const members = [
  {
    id: "user-jesper",
    name: "Jesper Hvejsel",
    email: "jesper@example.com",
    role: "admin",
  },
  {
    id: "user-sara",
    name: "Sara",
    email: "sara@example.com",
    role: "member",
  },
];

const agents = [
  {
    id: "agent-lone",
    name: "Lone",
    ownerId: "user-jesper",
    runtimeId: "runtime-cloud",
  },
  {
    id: "agent-mia",
    name: "Mia",
    ownerId: "user-jesper",
    runtimeId: "runtime-local",
  },
];

const runtimes = [
  {
    id: "runtime-cloud",
    name: "Codex Cloud",
    ownerId: "user-jesper",
  },
  {
    id: "runtime-local",
    name: "Local Mac mini",
    ownerId: "user-sara",
  },
];

describe("permission audit model", () => {
  it("creates one resolver context per owned agent and a user-only fallback", () => {
    expect(
      buildPermissionAuditContexts({ members, agents, runtimes }),
    ).toEqual([
      {
        id: "user-jesper:agent-lone:runtime-cloud",
        userId: "user-jesper",
        agentId: "agent-lone",
        runtimeId: "runtime-cloud",
        label: "Lone",
      },
      {
        id: "user-jesper:agent-mia:runtime-local",
        userId: "user-jesper",
        agentId: "agent-mia",
        runtimeId: "runtime-local",
        label: "Mia",
      },
      {
        id: "user-sara::runtime-local",
        userId: "user-sara",
        runtimeId: "runtime-local",
        label: "Local Mac mini",
      },
    ]);
  });

  it("lists every user and maps each explicit permission layer to its owner", () => {
    const contexts = buildPermissionAuditContexts({ members, agents, runtimes });
    const rows = buildPermissionAuditRows({
      members,
      agents,
      runtimes,
      groups: [
        {
          id: "group-leadership",
          name: "Leadership",
          userIds: ["user-jesper"],
        },
      ],
      holders: [
        { layer: "workspace", subject_id: "workspace", setting: "deny" },
        { layer: "runtime", subject_id: "runtime-cloud", setting: "ask" },
        { layer: "agent", subject_id: "agent-lone", setting: "allow" },
        { layer: "group", subject_id: "group-leadership", setting: "deny" },
        { layer: "user", subject_id: "user-jesper", setting: "allow" },
      ],
      contexts,
      resolvedByContext: new Map([
        [
          "user-jesper:agent-lone:runtime-cloud",
          { setting: "deny", decidedBy: "workspace", cappedBy: "" },
        ],
        [
          "user-jesper:agent-mia:runtime-local",
          { setting: "deny", decidedBy: "group", cappedBy: "group" },
        ],
        [
          "user-sara::runtime-local",
          { setting: "deny", decidedBy: "workspace", cappedBy: "" },
        ],
      ]),
    });

    expect(rows.map((row) => row.user.name)).toEqual([
      "Jesper Hvejsel",
      "Sara",
    ]);

    const jesper = rows[0]!;
    expect(jesper.workspace).toEqual([
      { id: "workspace", label: "Whole workspace", setting: "deny" },
    ]);
    expect(jesper.runtimes).toEqual([
      { id: "runtime-cloud", label: "Codex Cloud", setting: "ask" },
      { id: "runtime-local", label: "Local Mac mini", setting: null },
    ]);
    expect(jesper.agents).toEqual([
      { id: "agent-lone", label: "Lone", setting: "allow" },
      { id: "agent-mia", label: "Mia", setting: null },
    ]);
    expect(jesper.groups).toEqual([
      { id: "group-leadership", label: "Leadership", setting: "deny" },
    ]);
    expect(jesper.direct).toEqual([
      { id: "user-jesper", label: "Jesper Hvejsel", setting: "allow" },
    ]);
    expect(jesper.effective).toEqual([
      { contextLabel: "Lone", setting: "deny", source: "Workspace" },
      { contextLabel: "Mia", setting: "deny", source: "Group" },
    ]);

    const sara = rows[1]!;
    expect(sara.workspace[0]?.setting).toBe("deny");
    expect(sara.runtimes).toEqual([
      { id: "runtime-local", label: "Local Mac mini", setting: null },
    ]);
    expect(sara.agents).toEqual([]);
    expect(sara.groups).toEqual([]);
    expect(sara.direct).toEqual([
      { id: "user-sara", label: "Sara", setting: null },
    ]);
    expect(sara.effective).toEqual([
      {
        contextLabel: "Local Mac mini",
        setting: "deny",
        source: "Workspace",
      },
    ]);
  });
});
