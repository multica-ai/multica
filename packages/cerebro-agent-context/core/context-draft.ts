export interface ContextDraftFields {
  instructions: string;
  model: string;
  thinkingLevel: string;
  personaSandbox: string;
  // FIR-3212 brief-layer modes. Stored inside the agent's runtime_config, but
  // tracked here as flat draft fields so a change to either counts as dirty and
  // flows through the same propose→approve path as model / thinking / sandbox.
  workspaceBriefMode: string;
  toolsBriefMode: string;
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
