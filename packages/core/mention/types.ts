/**
 * Single authoritative union of all mention types in Multica.
 * Every consumer (frontend, backend test fixtures, mobile) imports from here.
 */
export type MentionType =
  | "member"
  | "agent"
  | "squad"
  | "issue"
  | "project"
  | "all"
  | "skill";

/** All known mention types as a runtime array. */
export const ALL_MENTION_TYPES: MentionType[] = [
  "member",
  "agent",
  "squad",
  "issue",
  "project",
  "all",
  "skill",
];

/**
 * Configuration for a single mention type.
 *
 * - `label` — human-readable display name.
 * - `prefix` — whether the mention is rendered with a `@` prefix.
 * - `groupLabel` — grouping key used in the suggestion dropdown.
 * - `behavior` — `"link"` navigates somewhere; `"trigger"` executes an action.
 * - `chipVariant` — string key mapped to Tailwind classes by `packages/ui/` components.
 *   `packages/core/` must not import UI libraries, so this is intentionally a plain string.
 * - `actorType` — whether the mention type represents an actor (member, agent, squad, all).
 */
export interface MentionTypeConfig {
  label: string;
  prefix: boolean;
  groupLabel: string;
  behavior: "link" | "trigger";
  chipVariant: string;
  actorType: boolean;
}
