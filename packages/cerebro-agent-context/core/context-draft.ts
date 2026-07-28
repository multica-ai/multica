export interface ContextDraftFields {
  instructions: string;
  model: string;
  thinkingLevel: string;
  // FIR-3212 brief-layer modes. Stored inside the agent's runtime_config, but
  // tracked here as flat draft fields so a change to either counts as dirty and
  // flows through the same propose→approve path as model / thinking.
  workspaceBriefMode: string;
  toolsBriefMode: string;
  // FIR-3212: how the agent's prompt reaches the model — append to the
  // runtime's own system prompt, replace it, or splice it into the user
  // message. Stored in runtime_config next to the two brief-layer modes.
  systemPromptMode: string;
}

// FIR-3212: read the system-prompt delivery mode out of runtime_config.
//
// Kept separate from readBriefLayerMode because the vocabularies differ: the
// brief layers spell their default "full", this one has no such alias and its
// only valid non-default values are append / replace / prepend. Anything else —
// a number, a mode a later server version dropped — reads as the default, so
// the dialog shows the agent's real behaviour rather than a value the daemon
// would ignore. Mirrors SystemPromptModeOf on the server.
export function readSystemPromptMode(
  runtimeConfig: Record<string, unknown> | undefined | null,
): string {
  const raw = runtimeConfig?.["system_prompt_mode"];
  if (raw !== "append" && raw !== "replace" && raw !== "prepend") return "";
  return raw;
}

// FIR-3212: read a brief-layer mode out of an agent's runtime_config for the
// dialog's "current" value. runtime_config is free-form and owned by several
// features, so anything that is not a string reads as the default. "full" is
// the explicit spelling of the default and normalises to "", so the dialog
// never shows a change against an agent that merely spelled out the default —
// mirrors WorkspaceBriefModeOf/ToolsBriefModeOf on the server.
export function readBriefLayerMode(
  runtimeConfig: Record<string, unknown> | undefined | null,
  key: "workspace_brief_mode" | "tools_brief_mode",
): string {
  const raw = runtimeConfig?.[key];
  if (typeof raw !== "string" || raw === "full") return "";
  return raw;
}

export function bumpPatch(version: string): string {
  const match = /^(\d+)\.(\d+)\.(\d+)$/.exec(version);
  if (!match) return "1.0.1";
  return `${match[1]}.${match[2]}.${Number(match[3]) + 1}`;
}

export function isContextDraftDirty(
  current: ContextDraftFields,
  draft: ContextDraftFields,
): boolean {
  return Object.keys(current).some(
    (key) =>
      current[key as keyof ContextDraftFields] !==
      draft[key as keyof ContextDraftFields],
  );
}

export function canSubmitContextDraft(title: string, dirty: boolean): boolean {
  return dirty && title.trim().length > 0;
}
