import type { StoredAgentDraft } from "../types";
import { EMPTY_AGENT_DRAFT, type AgentDraft } from "./draft";

export type { StoredAgentDraft };

export function toStoredAgentDraft(
  draft: AgentDraft,
  appliedMessageId: string | null,
): StoredAgentDraft {
  return {
    name: draft.name,
    description: draft.description,
    instructions: draft.instructions,
    starter_prompts: draft.starterPrompts,
    avatar_url: draft.avatarUrl,
    model: draft.model,
    thinking_level: draft.thinkingLevel,
    service_tier: draft.serviceTier,
    skill_ids: [...draft.skillIds],
    permission_scope: draft.permissionScope,
    member_ids: [...draft.memberIds],
    team_ids: [...draft.teamIds],
    applied_message_id: appliedMessageId,
  };
}

/**
 * Rebuilds the editable draft. `runtimeId` comes from the caller because the
 * server owns it — see the note on {@link StoredAgentDraft}.
 */
export function fromStoredAgentDraft(
  stored: StoredAgentDraft,
  runtimeId: string,
): AgentDraft {
  return {
    ...EMPTY_AGENT_DRAFT,
    name: stored.name,
    description: stored.description,
    instructions: stored.instructions,
    starterPrompts: stored.starter_prompts ?? [],
    avatarUrl: stored.avatar_url,
    runtimeId,
    model: stored.model,
    thinkingLevel: stored.thinking_level,
    serviceTier: stored.service_tier,
    skillIds: new Set(stored.skill_ids),
    permissionScope: stored.permission_scope,
    memberIds: new Set(stored.member_ids),
    teamIds: new Set(stored.team_ids),
  };
}

/**
 * Whether two drafts would serialize identically — the autosave's "has anything
 * actually changed" test. Compared on the stored form so a `Set` rebuilt with
 * the same members does not read as an edit and cause a pointless write.
 */
export function storedAgentDraftsEqual(
  a: StoredAgentDraft,
  b: StoredAgentDraft,
): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}
