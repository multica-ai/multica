// Agent Office (FIR-1775) — normalises a versioned context snapshot into an
// ordered, labelled list of fields so the UI can SHOW and DIFF the whole
// bundle field-by-field, not just the instructions blob. The backend already
// versions every field (instructions, model, thinking, sandbox, skills,
// mcp_config, custom_args, runtime_config, secret NAMES); this is the frontend
// view of that same bundle.

import type { AgentContextSnapshot } from "@multica/core/types";
import { maskSecretsDeep } from "./mask-secrets";

export interface SnapshotField {
  /** Stable key (matches the snapshot property). */
  key: string;
  /** Human label shown in the UI. */
  label: string;
  /** Normalised display/diff text. Empty string means "not set". */
  value: string;
  /** Render in a monospace block (code / config / lists). */
  mono: boolean;
}

// Free-form JSON fields can embed raw auth tokens; mask secret-looking values
// before they are rendered or diffed so the version history / Propose-change
// modal never shows a plaintext secret. See ./mask-secrets.
function jsonToText(v: unknown): string {
  if (v == null) return "";
  if (typeof v === "string") return maskSecretsDeepString(v);
  if (Array.isArray(v) && v.length === 0) return "";
  if (typeof v === "object" && Object.keys(v as object).length === 0) return "";
  try {
    return JSON.stringify(maskSecretsDeep(v), null, 2);
  } catch {
    return String(v);
  }
}

function maskSecretsDeepString(s: string): string {
  const masked = maskSecretsDeep(s);
  return typeof masked === "string" ? masked.trim() : s.trim();
}

function listToText(v: string[] | undefined | null): string {
  if (!v || v.length === 0) return "";
  return v.join("\n");
}

const VERSIONED_RUNTIME_KEYS = [
  "system_prompt_mode",
  "workspace_brief_mode",
  "tools_brief_mode",
  "speed_mode",
  "max_turns",
  "timeout_minutes",
] as const;

function runtimeConfigRecord(
  value: unknown,
): Record<string, unknown> | null {
  return value != null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function runtimeString(
  config: Record<string, unknown> | null,
  key: string,
  accepted: string[],
): string {
  const raw = config?.[key];
  return typeof raw === "string" && accepted.includes(raw) ? raw : "";
}

function runtimePositiveInteger(
  config: Record<string, unknown> | null,
  key: string,
): string {
  const raw = config?.[key];
  return typeof raw === "number" && Number.isInteger(raw) && raw > 0
    ? String(raw)
    : "";
}

function runtimeConfigRest(value: unknown): unknown {
  const config = runtimeConfigRecord(value);
  if (!config) return value;
  const rest = { ...config };
  for (const key of VERSIONED_RUNTIME_KEYS) delete rest[key];
  return rest;
}

// Skills are stored as opaque UUIDs. When a resolver is supplied (the view
// layer has the workspace skill list loaded) we render the human-readable
// skill name instead of the raw id, falling back to the id only for a skill
// that no longer resolves. See useSkillNameResolver.
function skillsToText(
  ids: string[] | undefined | null,
  resolveSkill?: (id: string) => string,
): string {
  if (!ids || ids.length === 0) return "";
  if (!resolveSkill) return ids.join("\n");
  return ids.map((id) => resolveSkill(id)).join("\n");
}

// snapshotFieldsChanged reports whether any of the given snapshot keys differ
// between two snapshots, using the same normalisation the diff view uses. The
// Skills / MCP tabs use it to show only the change requests and versions that
// actually touch their field, instead of every context change.
export function snapshotFieldsChanged(
  base: AgentContextSnapshot,
  proposed: AgentContextSnapshot,
  keys: string[],
): boolean {
  const baseFields = snapshotToFields(base);
  const proposedFields = snapshotToFields(proposed);
  return keys.some((key) => {
    const b = baseFields.find((f) => f.key === key)?.value ?? "";
    const p = proposedFields.find((f) => f.key === key)?.value ?? "";
    return b !== p;
  });
}

export interface SnapshotToFieldsOptions {
  /** Maps a skill id to its human-readable name; id passed through if unknown. */
  resolveSkill?: (id: string) => string;
}

// changedSnapshotKeys names every snapshot key whose normalised value differs
// between two snapshots.
//
// FIR-3212: the approval panel asks the server what each changed field MEANS on
// the agent's engine, and it must ask about exactly the fields the diff beside it
// renders — so both read this one predicate. Pass the same resolveSkill the diff
// uses, otherwise a renamed skill is "changed" in one and not the other.
export function changedSnapshotKeys(
  base: AgentContextSnapshot,
  proposed: AgentContextSnapshot,
  opts: SnapshotToFieldsOptions = {},
): string[] {
  const baseFields = snapshotToFields(base, opts);
  const proposedFields = snapshotToFields(proposed, opts);
  return baseFields
    .filter((bf, i) => (proposedFields[i]?.value ?? "") !== bf.value)
    .map((bf) => bf.key);
}

// Field order is deliberate: what a person reads first (instructions, model,
// thinking) before the structured config (skills, secrets, configs).
export function snapshotToFields(
  s: AgentContextSnapshot,
  opts: SnapshotToFieldsOptions = {},
): SnapshotField[] {
  // Skills render as names (not UUIDs) when resolved, so they no longer need
  // the monospace treatment reserved for ids/config.
  const skillsResolved = Boolean(opts.resolveSkill);
  const runtimeConfig = runtimeConfigRecord(s.runtime_config);
  return [
    { key: "instructions", label: "Instructions", value: (s.instructions ?? "").trim(), mono: false },
    { key: "description", label: "Short description", value: (s.description ?? "").trim(), mono: false },
    { key: "runtime_id", label: "Engine", value: (s.runtime_id ?? "").trim(), mono: true },
    { key: "model", label: "Model", value: (s.model ?? "").trim(), mono: true },
    { key: "thinking_level", label: "Thinking level", value: (s.thinking_level ?? "").trim(), mono: true },
    { key: "system_prompt_mode", label: "System prompt", value: runtimeString(runtimeConfig, "system_prompt_mode", ["append", "replace", "prepend"]), mono: true },
    { key: "workspace_brief_mode", label: "Shared brief", value: runtimeString(runtimeConfig, "workspace_brief_mode", ["off"]), mono: true },
    { key: "tools_brief_mode", label: "Tools list", value: runtimeString(runtimeConfig, "tools_brief_mode", ["summary"]), mono: true },
    { key: "speed_mode", label: "Speed", value: runtimeString(runtimeConfig, "speed_mode", ["fast"]), mono: true },
    { key: "max_turns", label: "Stop after", value: runtimePositiveInteger(runtimeConfig, "max_turns"), mono: true },
    { key: "timeout_minutes", label: "Give up after", value: runtimePositiveInteger(runtimeConfig, "timeout_minutes"), mono: true },
    { key: "skill_ids", label: "Skills", value: skillsToText(s.skill_ids, opts.resolveSkill), mono: !skillsResolved },
    // FIR-3805: its own field, and always present in the list. The queue and the
    // version history keep only the requests whose changed keys intersect the
    // tab's keys, so a proposal that ONLY flips "always on" was filtered out of
    // the Skills tab — stored server-side, invisible, never approved. Emitting
    // the row unconditionally also keeps the index-aligned comparison in
    // changedSnapshotKeys honest: a row that appears in one snapshot and not the
    // other would shift every field after it.
    { key: "always_on_skill_ids", label: "Always-on skills", value: skillsToText(s.always_on_skill_ids, opts.resolveSkill), mono: !skillsResolved },
    { key: "custom_env_keys", label: "Secret names", value: listToText(s.custom_env_keys), mono: true },
    { key: "mcp_config", label: "MCP config", value: jsonToText(s.mcp_config), mono: true },
    { key: "custom_args", label: "Custom args", value: jsonToText(s.custom_args), mono: true },
    { key: "runtime_config", label: "Other runtime config", value: jsonToText(runtimeConfigRest(s.runtime_config)), mono: true },
  ];
}
