export interface ContextDraftFields {
  instructions: string;
  model: string;
  thinkingLevel: string;
  personaSandbox: string;
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
