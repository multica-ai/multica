import type { AnalyticsDimension, AnalyticsOperator } from "@multica/cerebro-usage";

// UI wording for analytics dimensions. Internal field names (person, source,
// cost_kind …) must never leak into the interface — every label here uses the
// word the rest of the app already uses for the same concept.
const DIMENSION_LABELS: Record<AnalyticsDimension, string> = {
  time: "Time",
  person: "Member",
  agent: "Agent",
  project: "Project",
  runtime: "Runtime",
  source: "Triggered from",
  provider: "Provider",
  model: "Model",
  skill: "Skill",
  status: "Status",
  cost_kind: "Cost data",
  quality_type: "Signal source",
  quality_category: "Signal category",
  context: "Context",
  run: "Run",
  issue: "Issue",
  source_id: "Trigger ID",
  reference: "Reference",
  reference_label: "Context",
  debug_link: "Debug link",
  trace: "Trace",
  function: "Function",
  operating_loop: "Operating Loop",
};

// What an empty value means, per dimension. Empty is a real, filterable state
// (the backend matches '' against unset values) — never render it as the
// meaningless "Unknown".
const EMPTY_VALUE_LABELS: Partial<Record<AnalyticsDimension, string>> = {
  person: "No member (automated)",
  agent: "No agent",
  project: "No project",
  runtime: "No runtime",
  source: "Manual",
  provider: "No provider",
  model: "No model",
  skill: "No skill",
  status: "No status",
  cost_kind: "No cost data",
  issue: "No issue",
};

// Trigger values are stored as internal source types; show the app's words.
const SOURCE_VALUE_LABELS: Record<string, string> = {
  issue: "Issue",
  chat: "Chat",
  dm: "DM",
  channel: "Channel",
  autopilot: "Autopilot",
  manual: "Manual",
};

const OPERATOR_LABELS: Record<AnalyticsOperator, string> = {
  in: "is",
  not_in: "is not",
  eq: "is",
  gte: "≥",
  lte: "≤",
  contains: "contains",
  not_contains: "does not contain",
};

export function dimensionLabel(dimension: AnalyticsDimension | string): string {
  return DIMENSION_LABELS[dimension as AnalyticsDimension] ?? String(dimension).replaceAll("_", " ");
}

export function operatorLabel(operator: AnalyticsOperator): string {
  return OPERATOR_LABELS[operator] ?? operator;
}

export function valueLabel(dimension: AnalyticsDimension | string, value: string | null | undefined): string {
  if (value == null || value === "") {
    return EMPTY_VALUE_LABELS[dimension as AnalyticsDimension] ?? "None";
  }
  if (dimension === "source") return SOURCE_VALUE_LABELS[value] ?? value;
  return value;
}

// Dimensions whose value space is a small enumerable set, where the filter
// builder should offer a picker of real values instead of a free-text field.
export const ENUMERABLE_DIMENSIONS: AnalyticsDimension[] = [
  "person",
  "agent",
  "project",
  "runtime",
  "source",
  "provider",
  "model",
  "skill",
  "status",
  "cost_kind",
  "quality_type",
  "quality_category",
  "issue",
];
