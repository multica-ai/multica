import { describe, expect, it } from "vitest";
import type { Autopilot } from "../types";
import {
  filterMineAutopilots,
  isMineAutopilot,
  ownedAgentIdsForUser,
} from "./ownership";

function autopilot(overrides: Partial<Autopilot>): Autopilot {
  return {
    id: overrides.id ?? "ap-1",
    workspace_id: "ws-1",
    title: "Autopilot",
    description: null,
    project_id: null,
    assignee_type: "agent",
    assignee_id: "agent-other",
    status: "active",
    execution_mode: "run_only",
    issue_title_template: null,
    manual_options: [],
    created_by_type: "member",
    created_by_id: "user-other",
    last_run_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("autopilot ownership", () => {
  it("derives owned agent ids for the current user", () => {
    expect(
      ownedAgentIdsForUser(
        [
          { id: "agent-mine", owner_id: "user-me" },
          { id: "agent-other", owner_id: "user-other" },
          { id: "agent-unowned", owner_id: null },
        ],
        "user-me",
      ),
    ).toEqual(["agent-mine"]);
  });

  it("matches current user and owned-agent creator or assignee", () => {
    const context = {
      currentUserId: "user-me",
      ownedAgentIds: ["agent-mine"],
    };

    expect(
      isMineAutopilot(
        autopilot({ created_by_type: "member", created_by_id: "user-me" }),
        context,
      ),
    ).toBe(true);
    expect(
      isMineAutopilot(
        autopilot({ created_by_type: "agent", created_by_id: "agent-mine" }),
        context,
      ),
    ).toBe(true);
    expect(
      isMineAutopilot(
        autopilot({ assignee_type: "agent", assignee_id: "agent-mine" }),
        context,
      ),
    ).toBe(true);
    expect(
      isMineAutopilot(
        autopilot({
          created_by_type: "member",
          created_by_id: "user-other",
          assignee_type: "agent",
          assignee_id: "agent-other",
        }),
        context,
      ),
    ).toBe(false);
  });

  it("matches squad assignees only when the squad leader is an owned agent", () => {
    const context = {
      currentUserId: "user-me",
      ownedAgentIds: ["agent-mine"],
      squads: [
        { id: "squad-mine", leader_id: "agent-mine" },
        { id: "squad-other", leader_id: "agent-other" },
      ],
    };

    expect(
      isMineAutopilot(
        autopilot({ assignee_type: "squad", assignee_id: "squad-mine" }),
        context,
      ),
    ).toBe(true);
    expect(
      isMineAutopilot(
        autopilot({ assignee_type: "squad", assignee_id: "squad-other" }),
        context,
      ),
    ).toBe(false);
  });

  it("filters full lists with the same ownership rules", () => {
    const rows = [
      autopilot({ id: "member", created_by_id: "user-me" }),
      autopilot({
        id: "creator-agent",
        created_by_type: "agent",
        created_by_id: "agent-mine",
      }),
      autopilot({ id: "assignee-agent", assignee_id: "agent-mine" }),
      autopilot({
        id: "assignee-squad",
        assignee_type: "squad",
        assignee_id: "squad-mine",
      }),
      autopilot({ id: "other" }),
    ];

    expect(
      filterMineAutopilots(rows, {
        currentUserId: "user-me",
        ownedAgentIds: ["agent-mine"],
        squads: [{ id: "squad-mine", leader_id: "agent-mine" }],
      }).map((row) => row.id),
    ).toEqual(["member", "creator-agent", "assignee-agent", "assignee-squad"]);
  });
});
