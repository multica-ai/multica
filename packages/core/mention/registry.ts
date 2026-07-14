import type { MentionType, MentionTypeConfig } from "./types";

/**
 * Declarative registry mapping every {@link MentionType} to its configuration.
 *
 * Follows the `Record<EnumType, Config>` pattern from
 * `packages/core/issues/config/status.ts` (STATUS_CONFIG).
 */
export const MENTION_TYPE_REGISTRY: Record<MentionType, MentionTypeConfig> = {
  member: {
    label: "Member",
    prefix: true,
    groupLabel: "Users",
    behavior: "link",
    chipVariant: "member",
    actorType: true,
  },
  agent: {
    label: "Agent",
    prefix: true,
    groupLabel: "Users",
    behavior: "link",
    chipVariant: "agent",
    actorType: true,
  },
  squad: {
    label: "Squad",
    prefix: true,
    groupLabel: "Users",
    behavior: "link",
    chipVariant: "squad",
    actorType: true,
  },
  issue: {
    label: "Issue",
    prefix: false,
    groupLabel: "Issues",
    behavior: "link",
    chipVariant: "issue",
    actorType: false,
  },
  project: {
    label: "Project",
    prefix: false,
    groupLabel: "Issues",
    behavior: "link",
    chipVariant: "project",
    actorType: false,
  },
  all: {
    label: "All",
    prefix: true,
    groupLabel: "Users",
    behavior: "link",
    chipVariant: "all",
    actorType: true,
  },
  skill: {
    label: "Skill",
    prefix: true,
    groupLabel: "Skills",
    behavior: "trigger",
    chipVariant: "skill",
    actorType: false,
  },
};
