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

export interface SnapshotToFieldsOptions {
  /** Maps a skill id to its human-readable name; id passed through if unknown. */
  resolveSkill?: (id: string) => string;
}

// Field order is deliberate: what a person reads first (instructions, model,
// thinking, sandbox) before the structured config (skills, secrets, configs).
export function snapshotToFields(
  s: AgentContextSnapshot,
  opts: SnapshotToFieldsOptions = {},
): SnapshotField[] {
  // Skills render as names (not UUIDs) when resolved, so they no longer need
  // the monospace treatment reserved for ids/config.
  const skillsResolved = Boolean(opts.resolveSkill);
  return [
    { key: "instructions", label: "Instructions", value: (s.instructions ?? "").trim(), mono: false },
    { key: "description", label: "Short description", value: (s.description ?? "").trim(), mono: false },
    { key: "model", label: "Model", value: (s.model ?? "").trim(), mono: true },
    { key: "thinking_level", label: "Thinking level", value: (s.thinking_level ?? "").trim(), mono: true },
    { key: "persona_sandbox", label: "Sandbox", value: (s.persona_sandbox ?? "").trim(), mono: true },
    { key: "skill_ids", label: "Skills", value: skillsToText(s.skill_ids, opts.resolveSkill), mono: !skillsResolved },
    { key: "custom_env_keys", label: "Secret names", value: listToText(s.custom_env_keys), mono: true },
    { key: "mcp_config", label: "MCP config", value: jsonToText(s.mcp_config), mono: true },
    { key: "custom_args", label: "Custom args", value: jsonToText(s.custom_args), mono: true },
    { key: "runtime_config", label: "Runtime config", value: jsonToText(s.runtime_config), mono: true },
  ];
}
