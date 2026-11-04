import { isLocalPermissionMode, isLocalReasoningEffort, type LocalPermissionMode, type LocalReasoningEffort } from "@multica/core/local-conversation/protocol";

function preferenceKey(agentId: string, sessionId?: string | null) {
  return sessionId ? `session:${sessionId}` : `draft:${agentId}`;
}

function validModel(value: unknown): value is string {
  return typeof value === "string" && value.trim() === value && value.length > 0
    // eslint-disable-next-line no-control-regex -- A model id with control characters is exactly what this refuses.
    && new TextEncoder().encode(value).length <= 256 && !/[\x00-\x1f\x7f]/.test(value);
}

export class ModelPreferences {
  private values = new Map<string, string | null>();

  hydrate(raw: string | null) {
    if (!raw) return;
    try {
      const parsed: unknown = JSON.parse(raw);
      if (!Array.isArray(parsed)) return;
      for (const entry of parsed) {
        if (Array.isArray(entry) && typeof entry[0] === "string" && (entry[1] === null || validModel(entry[1]))) this.values.set(entry[0], entry[1]);
      }
    } catch { /* A damaged local preference must not block conversation history. */ }
  }

  get(agentId: string, sessionId?: string | null): string | null {
    return this.values.get(preferenceKey(agentId, sessionId)) ?? null;
  }
  has(agentId: string, sessionId?: string | null): boolean {
    return this.values.has(preferenceKey(agentId, sessionId));
  }

  set(agentId: string, sessionId: string | null | undefined, model: string | null) {
    if (model !== null && !validModel(model)) throw new Error("Invalid model ID");
    this.values.set(preferenceKey(agentId, sessionId), model);
  }

  serialize() { return JSON.stringify([...this.values]); }
}

interface ReasoningSelection { model: string | null; effort: LocalReasoningEffort | null }

export class ReasoningPreferences {
  private values = new Map<string, ReasoningSelection>();

  hydrate(raw: string | null) {
    if (!raw) return;
    try {
      const parsed: unknown = JSON.parse(raw);
      if (!Array.isArray(parsed)) return;
      for (const entry of parsed) {
        if (!Array.isArray(entry) || typeof entry[0] !== "string") continue;
        const value = entry[1];
        if (value && (value.model === null || typeof value.model === "string") && (value.effort === null || isLocalReasoningEffort(value.effort))) this.values.set(entry[0], value);
      }
    } catch { /* Damaged preferences must not block conversation history. */ }
  }

  get(agentId: string, sessionId?: string | null) { return this.values.get(preferenceKey(agentId, sessionId)); }
  set(agentId: string, sessionId: string | null | undefined, model: string | null, effort: LocalReasoningEffort | null) {
    this.values.set(preferenceKey(agentId, sessionId), { model, effort });
  }
  serialize() { return JSON.stringify([...this.values]); }
}

export class PermissionPreferences {
  private values = new Map<string, LocalPermissionMode>();

  hydrate(raw: string | null) {
    if (!raw) return;
    try {
      const entries: unknown = JSON.parse(raw);
      if (Array.isArray(entries)) for (const entry of entries) {
        if (Array.isArray(entry) && typeof entry[0] === "string" && isLocalPermissionMode(entry[1])) this.values.set(entry[0], entry[1]);
      }
    } catch { /* Invalid saved preferences retain the provider's defaults. */ }
  }

  get(agentId: string, sessionId?: string | null) { return this.values.get(preferenceKey(agentId, sessionId)); }
  set(agentId: string, sessionId: string | null | undefined, mode: LocalPermissionMode) {
    if (!isLocalPermissionMode(mode)) throw new Error("Invalid permission mode");
    this.values.set(preferenceKey(agentId, sessionId), mode);
  }
  serialize() { return JSON.stringify([...this.values]); }
}
