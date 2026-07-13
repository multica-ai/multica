import type { MentionType } from "./types";
import { MENTION_TYPE_REGISTRY } from "./registry";

/**
 * Returns whether the given mention type should be rendered with a `@` prefix.
 *
 * `true` for member, agent, squad, all, skill.
 * `false` for issue, project.
 */
export function getMentionPrefix(type: MentionType): boolean {
  return MENTION_TYPE_REGISTRY[type].prefix;
}

/**
 * Returns the group label used to cluster mention types in the suggestion dropdown.
 *
 * - "Users" for member, agent, squad, all
 * - "Issues" for issue, project
 * - "Skills" for skill
 */
export function getMentionGroupLabel(type: MentionType): string {
  return MENTION_TYPE_REGISTRY[type].groupLabel;
}

/**
 * Returns whether the mention type represents an actor (someone who can be
 * assigned, can own tasks, and can appear in actor-specific UI).
 *
 * `true` for member, agent, squad, all.
 * `false` for issue, project, skill.
 */
export function isActorMentionType(type: MentionType): boolean {
  return MENTION_TYPE_REGISTRY[type].actorType;
}
