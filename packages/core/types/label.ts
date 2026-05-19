/**
 * Issue labels — workspace-scoped, applied as many-to-many to issues.
 *
 * Labels are lightweight metadata (name + color) distinct from projects:
 * projects group related work, labels are cross-cutting tags (bug, feature,
 * performance, …). Colors are normalized to lowercase `#RRGGBB`.
 *
 * Labels can optionally carry agent instructions. When an agent starts a
 * task on an issue with labeled instructions, those instructions are
 * appended to the agent's system prompt — giving the agent label-aware
 * context without changing the assignment model.
 */
export interface Label {
  id: string;
  workspace_id: string;
  name: string;
  /** Normalized lowercase hex color, e.g. `#3b82f6`. */
  color: string;
  /** Optional agent instructions injected into the prompt when this label
   *  is attached to an issue. Empty string means no instructions. */
  instructions: string;
  created_at: string;
  updated_at: string;
}

export interface CreateLabelRequest {
  name: string;
  color: string;
  instructions?: string;
}

export interface UpdateLabelRequest {
  name?: string;
  color?: string;
  instructions?: string;
}

export interface ListLabelsResponse {
  labels: Label[];
  total: number;
}

export interface IssueLabelsResponse {
  labels: Label[];
}
