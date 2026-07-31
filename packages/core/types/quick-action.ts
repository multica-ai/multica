/**
 * Issue Quick Actions (MUL-5465) — workspace-level presets for "who to call
 * and what to say" on an existing issue.
 *
 * Running one is not a separate dispatch path: the server renders the prompt,
 * posts a `quick_action` comment carrying the target's mention markup, and the
 * normal comment -> mention -> task trigger takes over. That is why the run
 * response is a `Comment` with `trigger_outcomes` rather than a bespoke shape.
 */

/**
 * The author's stated intent, chosen at creation.
 *
 * - `public`  — meant for the team. The server requires a target every member
 *   can invoke, so it is runnable by everyone by construction.
 * - `private` — meant for its creator only; any target is allowed, and the
 *   list endpoint returns it to nobody else.
 *
 * This is intent, NOT an authorization decision — the run endpoint always
 * re-checks invoke permission. Server-driven enum: switch with a `default`.
 */
export type QuickActionVisibility = "private" | "public";

export type QuickActionAssigneeType = "agent" | "squad";

export type QuickActionStatus = "active" | "archived";

export interface QuickAction {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  assignee_type: QuickActionAssigneeType | string;
  assignee_id: string;
  /** Sent verbatim — there is no interpolation step. */
  prompt: string;
  visibility: QuickActionVisibility | string;
  status: QuickActionStatus | string;
  last_used_at: string | null;
  use_count: number;
  created_by_id: string;
  created_at: string;
  updated_at: string;
  /** Display name of the bound agent or squad. Absent when it no longer resolves. */
  target_name?: string;
  /**
   * Whether the bound target is currently invocable by every workspace member.
   * Plain metadata, not a verdict — settings shows it beside the binding so a
   * `public` action pointing at a now-private agent reads as visibly wrong.
   */
  target_public: boolean;
  /** The bound agent or squad was archived or deleted. */
  target_missing: boolean;
}

export interface CreateQuickActionRequest {
  name: string;
  description?: string;
  assignee_type: QuickActionAssigneeType;
  assignee_id: string;
  prompt: string;
  visibility?: QuickActionVisibility;
}

export interface UpdateQuickActionRequest {
  name?: string;
  description?: string;
  assignee_type?: QuickActionAssigneeType;
  assignee_id?: string;
  prompt?: string;
  visibility?: QuickActionVisibility;
  status?: QuickActionStatus;
}

export interface ListQuickActionsResponse {
  quick_actions: QuickAction[];
}

/**
 * How many actions the issue sidebar shows before the rest collapse behind
 * "More". Scarcity here is structural: a list that renders all 30 stops being
 * a shortlist and becomes a menu nobody reads.
 */
export const QUICK_ACTION_SIDEBAR_LIMIT = 5;

/**
 * Matches any `{{...}}`, mirroring the server's write-time rejection.
 *
 * Templating is not supported: every variable considered named something the
 * agent already had from the issue context. The check survives the feature so
 * a habitual token cannot land literally in an agent's instructions — the form
 * shows it inline instead of waiting for a 400.
 */
export const QUICK_ACTION_TEMPLATE_TOKEN_RE = /\{\{[^}]*\}\}/;

/** The first template token in a prompt, or null when there is none. */
export function findQuickActionTemplateToken(prompt: string): string | null {
  return prompt.match(QUICK_ACTION_TEMPLATE_TOKEN_RE)?.[0] ?? null;
}
