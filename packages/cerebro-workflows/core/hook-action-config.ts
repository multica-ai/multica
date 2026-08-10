import { HOOK_ACTION_CATALOG } from "./hook-action-catalog.generated";

export interface ActionFieldDefinition {
  key: string;
  label: string;
  input: "text" | "textarea" | "number" | "datetime-local" | "checkbox" | "target" | "select";
  required?: boolean;
  target?: "agent" | "member" | "issue" | "workflow" | "skill" | "squad" | "artifact" | "eval";
  summary: "target" | "safe" | "redacted" | "omit";
  help?: string;
  options?: ReadonlyArray<{ value: string; label: string }>;
  // Set when the field accepts a symbolic value resolved per event, such as
  // "the agent that triggered this hook" instead of one agent picked up front.
  event_target?: string;
}

export interface ActionDefinition {
  label: string;
  description: string;
  fields: readonly ActionFieldDefinition[];
}

// Lives in its own module so hook-ux and hook-validation can both read the
// action catalog without importing each other.
export const ACTION_CONFIGURATION = Object.fromEntries(HOOK_ACTION_CATALOG.map(({ type, label, description, fields }) => [type, { label, description, fields }])) as unknown as Record<string, ActionDefinition>;
export const HOOK_ACTION_OPTIONS = HOOK_ACTION_CATALOG.map(({ type, label }) => ({ value: type, label }));
