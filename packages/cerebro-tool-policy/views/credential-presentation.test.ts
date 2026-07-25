import { describe, expect, it } from "vitest";
import type { ToolPolicyRow } from "../core";
import {
  buildCredentialTree,
  groupCredentialRows,
  subtreeGroups,
} from "./credential-presentation";

function row(
  resource: string,
  category: string,
  toolKey: string,
): ToolPolicyRow {
  return {
    tool_key: toolKey,
    resource_pattern: resource,
    title: toolKey,
    category,
    source: "credential",
    managed_externally: false,
    layers: {
      workspace: null,
      runtime: null,
      agent: null,
      group: null,
      user: null,
      system: null,
    },
    conditions: {
      workspace: null,
      runtime: null,
      agent: null,
      user: null,
      system: null,
    },
    effective: {
      setting: "allow",
      decided_by: "",
      capped_by: "",
      reason: "Allowed by default",
      openable: true,
    },
    capped_by_groups: [],
  };
}

describe("credential presentation", () => {
  it("groups capability rows into stable credential boxes", () => {
    const groups = groupCredentialRows(
      [
        row("box:b", "shared-firtal-slack", "credential.rotate"),
        row("box:a", "agents-sofie-github", "credential.attach"),
        row("box:a", "agents-sofie-github", "credential.reveal"),
      ],
      "",
    );

    expect(groups.map((group) => group.label)).toEqual([
      "agents-sofie-github",
      "shared-firtal-slack",
    ]);
    expect(groups[0]!.rows.map((item) => item.tool_key)).toEqual([
      "credential.reveal",
      "credential.attach",
    ]);
  });

  it("builds track and owner branches without losing leaves", () => {
    const groups = groupCredentialRows(
      [
        row("box:a", "agents-sofie-github", "credential.reveal"),
        row("box:b", "agents-sofie-slack", "credential.reveal"),
        row("box:c", "default", "credential.reveal"),
      ],
      "",
    );
    const tree = buildCredentialTree(groups);
    const agentTrack = tree.find((node) => node.key === "track:agents");

    expect(agentTrack?.children[0]?.label).toBe("sofie");
    expect(agentTrack && subtreeGroups(agentTrack)).toHaveLength(2);
    expect(tree.find((node) => node.label === "default")?.group?.resource).toBe(
      "box:c",
    );
  });
});
