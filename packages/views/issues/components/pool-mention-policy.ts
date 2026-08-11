import type { MentionActorTarget } from "../../editor/extensions/mention-suggestion";
import type { CommentMentionPolicy } from "@multica/core/types";

type IssuePlacement = {
  assignee_type: "member" | "agent" | "squad" | null;
  assignee_id: string | null;
  creator_type: "member" | "agent" | "squad";
  creator_id: string;
};

type AgentPlacement = {
  id: string;
  comment_mention_policy?: CommentMentionPolicy;
  archived_at: string | null;
};

type SquadPlacement = {
  id: string;
  leader_id: string;
  archived_at: string | null;
};

export function getIssueMentionActorTarget(
  issue: IssuePlacement,
  agents: AgentPlacement[],
  squads: SquadPlacement[],
  currentUserID?: string,
): MentionActorTarget | null {
  const agentID = issue.assignee_type === "agent"
    ? issue.assignee_id
    : issue.assignee_type === "squad"
      ? squads.find((squad) => !squad.archived_at && squad.id === issue.assignee_id)?.leader_id ?? null
      : null;
  const usesRestrictedMentionPolicy = agents.some(
    (agent) =>
      !agent.archived_at &&
      agent.id === agentID &&
      agent.comment_mention_policy === "creator_only_for_non_creator",
  );
  if (issue.creator_type === "member" && issue.creator_id === currentUserID) return null;
  return usesRestrictedMentionPolicy ? { type: issue.creator_type, id: issue.creator_id } : null;
}
