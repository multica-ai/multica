import { describe, expect, it } from "vitest";
import { getIssueMentionActorTarget } from "./pool-mention-policy";

const baseIssue = {
  assignee_type: "agent" as const,
  assignee_id: "pool-agent",
  creator_type: "member" as const,
  creator_id: "creator",
};

describe("getIssueMentionActorTarget", () => {
  it("restricts a fixed agent when the explicit mention policy is enabled", () => {
    expect(getIssueMentionActorTarget(baseIssue, [
      {
        id: "pool-agent",
        comment_mention_policy: "creator_only_for_non_creator",
        archived_at: null,
      },
    ], [], "collaborator")).toEqual({ type: "member", id: "creator" });
  });

  it("does not infer the restriction from Pool binding when the policy is disabled", () => {
    expect(getIssueMentionActorTarget(baseIssue, [
      {
        id: "pool-agent",
        comment_mention_policy: "unrestricted",
        archived_at: null,
      },
    ], [], "collaborator")).toBeNull();
  });

  it("restricts an issue assigned to an agent with the creator-only policy", () => {
    expect(getIssueMentionActorTarget(baseIssue, [
      { id: "pool-agent", comment_mention_policy: "creator_only_for_non_creator", archived_at: null },
    ], [], "collaborator")).toEqual({ type: "member", id: "creator" });
  });

  it("does not restrict the issue creator", () => {
    expect(getIssueMentionActorTarget(baseIssue, [
      { id: "pool-agent", comment_mention_policy: "creator_only_for_non_creator", archived_at: null },
    ], [], "creator")).toBeNull();
  });

  it("restricts an issue assigned to a squad whose leader has the policy", () => {
    expect(getIssueMentionActorTarget({
      ...baseIssue,
      assignee_type: "squad",
      assignee_id: "pool-squad",
    }, [
      { id: "leader", comment_mention_policy: "creator_only_for_non_creator", archived_at: null },
    ], [
      { id: "pool-squad", leader_id: "leader", archived_at: null },
    ], "collaborator")).toEqual({ type: "member", id: "creator" });
  });

  it("does not restrict an issue assigned to an unrestricted agent", () => {
    expect(getIssueMentionActorTarget(baseIssue, [
      { id: "pool-agent", comment_mention_policy: "unrestricted", archived_at: null },
    ], [], "collaborator")).toBeNull();
  });
});
