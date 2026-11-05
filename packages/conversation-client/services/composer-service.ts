import { action, makeObservable, observable } from "mobx";
import { isLocalPermissionMode, isLocalReasoningEffort, localReasoningLevels, type LocalPermissionMode, type LocalReasoningEffort } from "@multica/core/local-conversation/protocol";
import type { RuntimeModelsResult } from "@multica/core/types";
import "../mobx-config";
import { GLOBAL_EXECUTION_PERMISSION_KEY, ModelPreferences, PermissionPreferences, ReasoningPreferences } from "../models/composer-preferences";

export type ComposerOptionsSection = "core" | "capabilities" | "connectors";
export type ComposerOptionsStatus = "loading" | "ready" | "error";

/** What merging sections of a target's options needs to know about them. */
export interface ComposerOptionsLike {
  provider: string;
  capabilities?: unknown;
  commands?: unknown;
  skills?: unknown;
  capabilityCatalog?: readonly { kind?: string }[] | null;
  slashCommandPolicy?: unknown;
  loadedAtUnixMs?: number;
}

export interface ComposerSettingsLike {
  model?: string | null;
  reasoningEffort?: string | null;
  speed?: string | null;
  planMode?: boolean | null;
  permissionModeId?: string | null;
}

export interface ComposerOptionsRequest {
  targetKey: string;
  provider: string;
  agentSessionId?: string | null;
  cwd?: string | null;
  settings?: ComposerSettingsLike | null;
  section?: ComposerOptionsSection;
  force?: boolean;
  waitForFreshModelCatalog?: boolean;
  signal?: AbortSignal;
}

const MODEL_PREFERENCES_KEY = "model-preferences-v1";
const REASONING_PREFERENCES_KEY = "reasoning-preferences-v1";
const SETTINGS_UPDATE_TIMEOUT_MS = 30_000;

/** A choice the service refused, with a stable code for callers. */
export class ComposerSelectionError extends Error {
  constructor(message: string, readonly code: string, readonly status?: number) {
    super(message);
    this.name = "ComposerSelectionError";
  }
}

export type ComposerSettingsPatch = Readonly<Record<string, unknown>>;
export type SessionSettingsStatus = "failed" | "inFlight" | "unknown" | "waitingForRuntime";

/** One conversation's unsettled write of its settings; none once it settles. */
export interface SessionSettingsUpdate {
  errorCode: string | null;
  errorMessage: string | null;
  queuedSettings: ComposerSettingsPatch | null;
  settings: ComposerSettingsPatch | null;
  status: SessionSettingsStatus;
}

interface SettingsRequest {
  /** A user's choices merge with their next; a new conversation's own patch does not. */
  kind: "activation" | "user";
  settings: ComposerSettingsPatch;
  timeoutMs?: number;
  settled?: { resolve(): void; reject(error: unknown): void };
}

interface SettingsLane extends SessionSettingsUpdate {
  current: SettingsRequest;
  queued: readonly SettingsRequest[];
}

export interface ComposerServiceOptions {
  storage: { read(key: string): Promise<string | null>; write(key: string, value: string): Promise<unknown>; subscribe?(key: string, listener: (value: string | null) => void): () => void };
  listModels(agentId: string, options?: { force?: boolean; signal?: AbortSignal }): Promise<RuntimeModelsResult>;
  /** The agent, if it is one of this workspace's. */
  agent(agentId: string): { model?: string | null; provider?: string } | undefined;
  /** The agent, or throws why it is not available. */
  requireAgent(agentId: string): { model?: string | null };
  /** The agent, or throws why this conversation cannot run on it. */
  assertInvokable(agentId: string, sessionId?: string): { model?: string | null };
  /** Throws once this workspace's connection is gone. */
  assertOpen(): void;
  /** What selections read of a conversation; detail is what its own record says, once loaded. */
  conversation(sessionId: string): { agentId: string; detail: { model?: string | null; reasoningEffort?: LocalReasoningEffort | null; permissionMode?: LocalPermissionMode; untouched: boolean } | null } | undefined;
  /** The id a conversation's choices are saved under. */
  storedId(sessionId: string): string;
  /** A run is under way, and its settings must not change beneath it. */
  busy(sessionId: string | null | undefined): boolean;
  /** Told a conversation's model or thinking effort changed. */
  selectionChanged(sessionId: string): void;
  /** What writing settings reads of a conversation; null when it has none here. */
  session(sessionId: string): { agentId: string | null; runtimeBlocked: boolean; settings: ComposerSettingsPatch | null | undefined } | null;
}

interface Entry<Options> {
  status: ComposerOptionsStatus;
  provider: string;
  loadingSignature: string | null;
  settledSignature: string | null;
  /** The load this entry waits for; a later one supersedes it. */
  inFlight: { promise: Promise<Options> } | null;
}

/**
 * Per-target model and reasoning choices, with one global permission preference.
 * Two requests with the same signature are interchangeable, so a settled result
 * answers a repeat and a load under way answers a duplicate.
 */
export class ComposerService<Options extends ComposerOptionsLike> {
  private readonly optionsByTarget = observable.map<string, Options>([], { deep: false });
  private readonly entries = observable.map<string, Entry<Options>>([], { deep: false });
  private readonly lanes = observable.map<string, SettingsLane>([], { deep: false });
  private readonly timers = new Set<ReturnType<typeof setTimeout>>();
  private readonly models = new ModelPreferences();
  private readonly reasoning = new ReasoningPreferences();
  private readonly permissions = new PermissionPreferences();
  private readonly catalogs = new Map<string, RuntimeModelsResult>();
  private readonly hydrations: { model: Promise<void>; reasoning: Promise<void>; permission: Promise<void> };
  private modelWrites: Promise<void> = Promise.resolve();
  private permissionWrites: Promise<void> = Promise.resolve();
  private readonly releasePermissions?: () => void;
  /** Changes with any model or thinking choice, and with each catalog read. */
  modelRevision = 0;
  /** Changes with any permission choice. */
  permissionRevision = 0;
  private disposed = false;

  constructor(private readonly loader: (request: ComposerOptionsRequest) => Promise<Options>, private readonly deps: ComposerServiceOptions) {
    makeObservable<ComposerService<Options>, "settle" | "begin" | "startSettings" | "settleSettings" | "advanceSettings" | "bumpModels" | "bumpPermissions">(this, {
      settle: action, begin: action, invalidate: action,
      modelRevision: observable, permissionRevision: observable, bumpModels: action, bumpPermissions: action,
      updateSettings: action, applyActivationSettings: action, reconcileSettings: action,
      startSettings: action, settleSettings: action, advanceSettings: action,
    });
    let permissionChanged = false;
    this.releasePermissions = deps.storage.subscribe?.(GLOBAL_EXECUTION_PERMISSION_KEY, (value) => {
      permissionChanged = true;
      this.receivePermissions(value);
    });
    this.hydrations = {
      model: deps.storage.read(MODEL_PREFERENCES_KEY).then((value) => { this.models.hydrate(value); }),
      reasoning: deps.storage.read(REASONING_PREFERENCES_KEY).then((value) => { this.reasoning.hydrate(value); }),
      permission: deps.storage.read(GLOBAL_EXECUTION_PERMISSION_KEY).then((value) => {
        if (!permissionChanged) this.receivePermissions(value);
      }),
    };
    for (const hydration of Object.values(this.hydrations)) void hydration.catch(() => {});
  }

  options(targetKey: string | null | undefined): Options | null {
    return this.optionsByTarget.get(targetKey?.trim() ?? "") ?? null;
  }

  status(targetKey: string | null | undefined, section?: ComposerOptionsSection): ComposerOptionsStatus | undefined {
    return this.entries.get(entryKey(targetKey?.trim() ?? "", section))?.status;
  }

  load(request: ComposerOptionsRequest): Promise<Options> {
    const { signal } = request;
    if (signal?.aborted) return Promise.reject(abortReason(signal));
    const answer = this.answer(request);
    if (!signal) return answer;
    return new Promise((resolve, reject) => {
      const onAbort = () => reject(abortReason(signal));
      signal.addEventListener("abort", onAbort, { once: true });
      answer.then(resolve, reject).finally(() => signal.removeEventListener("abort", onAbort));
    });
  }

  private answer(request: ComposerOptionsRequest): Promise<Options> {
    const targetKey = request.targetKey.trim();
    const provider = request.provider.trim();
    if (!targetKey || !provider) return Promise.reject(new Error("composer_options_load_not_accepted"));
    const signature = composerOptionsRequestSignature({ ...request, provider });
    const key = entryKey(targetKey, request.section);
    const current = this.entries.get(key);
    const known = this.optionsByTarget.get(targetKey);
    if (!request.force && current?.status === "ready" && current.settledSignature === signature && known) return Promise.resolve(known);
    if (current?.status === "loading" && current.loadingSignature === signature && current.inFlight) return current.inFlight.promise;
    return this.begin(request, targetKey, provider, signature, current);
  }

  private begin(request: ComposerOptionsRequest, targetKey: string, provider: string, signature: string, current: Entry<Options> | undefined): Promise<Options> {
    const inFlight = {} as { promise: Promise<Options> };
    inFlight.promise = this.loader(request).then(
      (options) => this.settle(targetKey, request.section, inFlight, options),
      () => this.settle(targetKey, request.section, inFlight, null),
    );
    this.setEntry(targetKey, request.section, { status: "loading", provider, loadingSignature: signature, settledSignature: current?.settledSignature ?? null, inFlight });
    return inFlight.promise;
  }

  private settle(targetKey: string, section: ComposerOptionsSection | undefined, inFlight: object, options: Options | null): Options {
    const current = this.entries.get(entryKey(targetKey, section));
    if (this.disposed) throw new Error("composer_service_disposed");
    if (current?.inFlight !== inFlight) throw new Error("composer_options_load_superseded");
    const settled = (status: ComposerOptionsStatus): Entry<Options> => ({
      ...current, status, loadingSignature: null, inFlight: null,
      settledSignature: status === "ready" ? current.loadingSignature : current.settledSignature,
    });
    if (!options || typeof options.provider !== "string") {
      this.setEntry(targetKey, section, settled("error"));
      throw new Error("composer_options_load_failed");
    }
    this.setEntry(targetKey, section, settled("ready"));
    const merged = section ? mergeComposerOptionsSection(this.optionsByTarget.get(targetKey), options, section) : options;
    this.optionsByTarget.set(targetKey, merged);
    return merged;
  }

  /** The core section is also the target's overall state. */
  private setEntry(targetKey: string, section: ComposerOptionsSection | undefined, entry: Entry<Options>) {
    this.entries.set(entryKey(targetKey, section), entry);
    if (section === "core") this.entries.set(entryKey(targetKey, undefined), entry);
  }

  /** Forgets what answers repeats; what is shown stays until a new load replaces it. */
  invalidate(filter: { providers?: readonly string[]; targetKeys?: readonly string[]; sections?: readonly ComposerOptionsSection[] } = {}): void {
    const { providers, targetKeys, sections } = filter;
    for (const [key, entry] of [...this.entries]) {
      const [targetKey, section] = key.split("\u0000") as [string, string];
      if (section && sections?.length && !sections.includes(section as ComposerOptionsSection)) continue;
      const anyFilter = providers?.length || targetKeys?.length;
      if (anyFilter && !providers?.includes(entry.provider) && !targetKeys?.includes(targetKey)) continue;
      this.entries.set(key, { ...entry, settledSignature: null, loadingSignature: null });
    }
  }

  dispose(): void {
    this.disposed = true;
    this.releasePermissions?.();
    for (const timer of this.timers) clearTimeout(timer);
    this.timers.clear();
  }

  // ---- Choices ----

  /** Saved choices have been read; a failed read rejects. */
  async hydrated(): Promise<void> {
    await Promise.all(Object.values(this.hydrations));
  }

  /** Every choice made so far has been saved or has failed. */
  async selectionsSettled(): Promise<void> {
    await this.modelsSettled();
    await this.hydrations.permission;
    await this.permissionWrites.catch(() => {});
  }

  async modelsSettled(): Promise<void> {
    await this.hydrations.model;
    await this.modelWrites.catch(() => {});
  }

  /** Claude's default used to be a catalog row. A saved one now means what no selection means, so it reaches the catalog's default model. */
  private followingClaudeDefault(agentId: string, model: string | null) {
    return model === "default" && this.deps.agent(agentId)?.provider === "claude";
  }

  private savedUnder(sessionId: string | null | undefined) {
    return sessionId ? this.deps.storedId(sessionId) : null;
  }

  selectedModel(agentId: string, sessionId?: string | null): string | null {
    const conversation = sessionId ? this.deps.conversation(sessionId) : undefined;
    if (conversation && conversation.agentId !== agentId) return null;
    const saved = this.savedUnder(sessionId);
    const model = this.models.has(agentId, saved) ? this.models.get(agentId, saved) : conversation?.detail?.model ?? null;
    return this.followingClaudeDefault(agentId, model) ? null : model;
  }

  selectedReasoning(agentId: string, sessionId?: string | null): LocalReasoningEffort | null {
    const conversation = sessionId ? this.deps.conversation(sessionId) : undefined;
    const detail = conversation?.detail ?? undefined;
    if (detail && conversation?.agentId !== agentId) return null;
    const model = this.selectedModel(agentId, sessionId);
    const selection = this.reasoning.get(agentId, this.savedUnder(sessionId));
    const effort = selection ? ((this.followingClaudeDefault(agentId, selection.model) ? null : selection.model) === model ? selection.effort : null)
      : model === (detail?.model ?? null) ? detail?.reasoningEffort ?? null : null;
    const catalog = this.catalogs.get(agentId);
    return effort && (!catalog || localReasoningLevels(catalog.models, model ?? this.deps.requireAgent(agentId).model).some((level) => level.value === effort)) ? effort : null;
  }

  selectedPermission(_agentId: string, _sessionId?: string | null): LocalPermissionMode {
    void this.permissionRevision;
    return this.permissions.get();
  }

  private receivePermissions(value: string | null): void {
    if (this.disposed) return;
    const previous = this.permissions.get();
    this.permissions.hydrate(value);
    if (previous !== this.permissions.get()) this.bumpPermissions();
  }

  async loadCatalog(agentId: string, options?: { force?: boolean; signal?: AbortSignal }): Promise<RuntimeModelsResult> {
    const catalog = await this.deps.listModels(agentId, options);
    this.catalogs.set(agentId, catalog);
    return catalog;
  }

  /** Reads the catalog for a picker, which redraws with what it lists. */
  async refreshCatalog(agentId: string, options?: { force?: boolean; signal?: AbortSignal }): Promise<RuntimeModelsResult> {
    const catalog = await this.loadCatalog(agentId, options);
    this.bumpModels();
    return catalog;
  }

  /** The effort to send with a prompt: checked against the current catalog when one is chosen. */
  async reasoningForSend(agentId: string, sessionId?: string | null): Promise<LocalReasoningEffort | null> {
    await this.hydrations.reasoning;
    await this.modelWrites.catch(() => {});
    if (this.selectedReasoning(agentId, sessionId)) await this.loadCatalog(agentId);
    return this.selectedReasoning(agentId, sessionId);
  }

  private bumpModels() { this.modelRevision++; }
  private bumpPermissions() { this.permissionRevision++; }

  private assertScope(agentId: string, sessionId: string | null | undefined, description: string) {
    if (!sessionId) return;
    const conversation = this.deps.conversation(sessionId);
    if (!conversation || conversation.agentId !== agentId) throw new ComposerSelectionError(description, "invalid_session", 409);
  }

  private assertIdle(sessionId: string | null | undefined, description: string) {
    if (this.deps.busy(sessionId)) throw new ComposerSelectionError(`Wait for this run to finish before changing ${description}.`, "run_in_progress", 409);
  }

  setModelSelection(agentId: string, sessionId: string | null | undefined, model: string | null): Promise<void> {
    const operation = this.modelWrites.catch(() => {}).then(async () => {
      await this.hydrations.model;
      this.deps.assertOpen();
      this.deps.assertInvokable(agentId, sessionId ?? undefined);
      this.assertScope(agentId, sessionId, "The model scope does not belong to this agent.");
      this.assertIdle(sessionId, "its model");
      const next = new ModelPreferences();
      next.hydrate(this.models.serialize());
      next.set(agentId, this.savedUnder(sessionId), model);
      await this.deps.storage.write(MODEL_PREFERENCES_KEY, next.serialize());
      this.deps.assertOpen();
      this.models.hydrate(next.serialize());
      if (sessionId) this.deps.selectionChanged(sessionId);
      this.bumpModels();
    });
    this.modelWrites = operation;
    return operation;
  }

  setReasoningSelection(agentId: string, sessionId: string | null | undefined, effort: string | null): Promise<void> {
    const operation = this.modelWrites.catch(() => {}).then(async () => {
      await this.hydrations.reasoning;
      await this.hydrations.model;
      this.deps.assertOpen();
      const agent = this.deps.assertInvokable(agentId, sessionId ?? undefined);
      if (effort !== null && !isLocalReasoningEffort(effort)) throw new ComposerSelectionError("Invalid thinking effort.", "invalid_reasoning_effort");
      this.assertScope(agentId, sessionId, "The thinking scope does not belong to this provider.");
      this.assertIdle(sessionId, "thinking effort");
      const model = this.selectedModel(agentId, sessionId);
      if (effort !== null && !localReasoningLevels((await this.loadCatalog(agentId)).models, model ?? agent.model).some((level) => level.value === effort)) throw new ComposerSelectionError("This model does not support that thinking effort.", "invalid_reasoning_effort");
      // The catalog request can overlap a newly accepted turn.
      this.assertIdle(sessionId, "thinking effort");
      const next = new ReasoningPreferences();
      next.hydrate(this.reasoning.serialize());
      next.set(agentId, this.savedUnder(sessionId), model, effort);
      await this.deps.storage.write(REASONING_PREFERENCES_KEY, next.serialize());
      this.deps.assertOpen();
      this.reasoning.hydrate(next.serialize());
      if (sessionId) this.deps.selectionChanged(sessionId);
      this.bumpModels();
    });
    this.modelWrites = operation;
    return operation;
  }

  setPermissionSelection(agentId: string, sessionId: string | null | undefined, mode: LocalPermissionMode): Promise<void> {
    const operation = this.permissionWrites.catch(() => {}).then(async () => {
      await this.hydrations.permission;
      this.deps.assertOpen();
      if (!isLocalPermissionMode(mode)) throw new ComposerSelectionError("Invalid local permission mode.", "invalid_permission_mode");
      this.deps.assertInvokable(agentId, sessionId ?? undefined);
      this.assertScope(agentId, sessionId, "The permission scope does not belong to this provider.");
      this.assertIdle(sessionId, "permissions");
      const next = new PermissionPreferences();
      next.hydrate(this.permissions.serialize());
      next.set(mode);
      await this.deps.storage.write(GLOBAL_EXECUTION_PERMISSION_KEY, next.serialize());
      this.deps.assertOpen();
      this.receivePermissions(next.serialize());
    });
    this.permissionWrites = operation;
    return operation;
  }

  /** Saves what a just-created conversation starts with: the draft's choices, or what its creation asked for. */
  async seedConversation(agent: { id: string; model?: string | null }, storedId: string, requested?: { model?: string | null; reasoningEffort?: string | null } | null): Promise<void> {
    const model = this.selectedModel(agent.id) ?? (requested?.model && requested.model !== agent.model ? requested.model : null);
    this.models.set(agent.id, storedId, model);
    await this.deps.storage.write(MODEL_PREFERENCES_KEY, this.models.serialize());
    await this.hydrations.reasoning;
    const effort = this.reasoning.get(agent.id) ? this.selectedReasoning(agent.id) : (isLocalReasoningEffort(requested?.reasoningEffort) ? requested.reasoningEffort : null);
    this.reasoning.set(agent.id, storedId, model, effort);
    await this.deps.storage.write(REASONING_PREFERENCES_KEY, this.reasoning.serialize());
  }

  // ---- Writing a conversation's settings ----

  /** What shows while a write is unsettled, and why the composer waits. */
  settingsUpdate(sessionId: string | null | undefined): SessionSettingsUpdate | null {
    return this.lanes.get(sessionId?.trim() ?? "") ?? null;
  }

  /**
   * The user changed settings. One write runs per conversation and later choices
   * merge behind it, so writes and sends never race. A choice after an unknown
   * outcome is the retry, carrying the settings that outcome left in doubt.
   */
  updateSettings(input: { agentSessionId: string; settings: ComposerSettingsPatch }): void {
    const id = input.agentSessionId.trim();
    const settings = { ...input.settings };
    if (!id || Object.keys(settings).length === 0) return;
    const session = this.deps.session(id);
    if (!session || session.runtimeBlocked) return;
    this.requestSettings(id, { kind: "user", settings, timeoutMs: SETTINGS_UPDATE_TIMEOUT_MS });
  }

  /** A patch a new conversation collected while being created. It waits its turn, even for a runtime that is away. */
  applyActivationSettings(sessionId: string, settings: ComposerSettingsPatch): Promise<void> {
    const id = sessionId.trim();
    if (!this.deps.session(id)) return Promise.reject(new ComposerSelectionError("Conversation is not loaded.", "session_not_loaded"));
    return new Promise((resolve, reject) => {
      this.requestSettings(id, { kind: "activation", settings: { ...settings }, settled: { resolve, reject } });
    });
  }

  private requestSettings(id: string, request: SettingsRequest) {
    const lane = this.lanes.get(id);
    if (!lane) { this.startSettings(id, request, []); return; }
    const queued = enqueueSettings(lane.queued, request);
    if (lane.status === "unknown") {
      const next = queued[0]!;
      this.startSettings(id, { ...next, settings: { ...lane.settings, ...next.settings } }, queued.slice(1));
    } else if (lane.status === "failed") {
      this.startSettings(id, queued[0]!, queued.slice(1));
    } else {
      this.lanes.set(id, withQueued(lane, queued));
    }
  }

  private startSettings(id: string, request: SettingsRequest, queued: readonly SettingsRequest[]) {
    const waiting = this.deps.session(id)?.runtimeBlocked === true;
    const lane = withQueued({
      current: request, queued, settings: { ...request.settings }, queuedSettings: null,
      status: waiting ? "waitingForRuntime" : "inFlight", errorCode: null, errorMessage: null,
    }, queued);
    this.lanes.set(id, lane);
    if (!waiting) this.runSettings(id, request);
  }

  private runSettings(id: string, request: SettingsRequest) {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const finished = () => { if (timer) { clearTimeout(timer); this.timers.delete(timer); } };
    if (request.timeoutMs !== undefined) {
      timer = setTimeout(() => { finished(); this.settleSettings(id, request, { outcome: "timedOut" }); }, request.timeoutMs);
      this.timers.add(timer);
    }
    this.applySettings(id, request.settings).then(
      () => { finished(); this.settleSettings(id, request, { outcome: "succeeded" }); },
      (error: unknown) => { finished(); this.settleSettings(id, request, { outcome: "failed", error }); },
    );
  }

  private async applySettings(id: string, settings: ComposerSettingsPatch) {
    this.deps.assertOpen();
    const agentId = this.deps.session(id)?.agentId;
    if (!agentId) throw new ComposerSelectionError("Conversation is not loaded.", "session_not_loaded");
    if (Object.keys(settings).some((key) => key !== "model" && key !== "reasoningEffort")) throw new ComposerSelectionError("Non-model conversation settings is not supported by this Multica connection.", "unsupported");
    if ("model" in settings) await this.setModelSelection(agentId, id, (settings.model as string | null | undefined) ?? null);
    if ("reasoningEffort" in settings) await this.setReasoningSelection(agentId, id, (settings.reasoningEffort as string | null | undefined) ?? null);
  }

  private settleSettings(id: string, request: SettingsRequest, result: { outcome: "succeeded" | "timedOut" } | { outcome: "failed"; error: unknown }) {
    // A timed-out write that answers late, or anything after the service stopped.
    const lane = this.lanes.get(id);
    if (this.disposed || lane?.current !== request || lane.status !== "inFlight") return;
    if (result.outcome === "succeeded") {
      this.advanceSettings(id, lane);
      request.settled?.resolve();
      return;
    }
    const failure = result.outcome === "failed" ? result.error : null;
    this.lanes.set(id, {
      ...lane,
      errorCode: result.outcome === "failed" ? errorCode(failure) : null,
      errorMessage: result.outcome === "failed" ? errorMessage(failure) : null,
      status: result.outcome === "timedOut" ? "unknown" : "failed",
    });
    request.settled?.reject(result.outcome === "failed" ? failure : new Error("settings update timed out"));
  }

  private advanceSettings(id: string, lane: SettingsLane) {
    const [next, ...queued] = lane.queued;
    if (next) this.startSettings(id, next, queued);
    else this.lanes.delete(id);
  }

  /**
   * The conversations changed. A write of unknown outcome that they now show has
   * landed after all, and a write waiting for its runtime may be able to go.
   */
  reconcileSettings(): void {
    if (this.disposed || !this.lanes.size) return;
    for (const [id, lane] of [...this.lanes]) {
      const session = this.deps.session(id);
      if (!session) continue;
      if (lane.status === "unknown") {
        if (settingsMatch(session.settings, lane.settings)) this.advanceSettings(id, lane);
      } else if (lane.status === "waitingForRuntime" && !session.runtimeBlocked) {
        this.lanes.set(id, { ...lane, status: "inFlight" });
        this.runSettings(id, lane.current);
      }
    }
  }
}

function enqueueSettings(queued: readonly SettingsRequest[], request: SettingsRequest): readonly SettingsRequest[] {
  const last = queued.at(-1);
  if (last?.kind === "user" && request.kind === "user") {
    return [...queued.slice(0, -1), { ...request, settings: { ...last.settings, ...request.settings } }];
  }
  return [...queued, request];
}

function withQueued(lane: SettingsLane, queued: readonly SettingsRequest[]): SettingsLane {
  return { ...lane, queued, queuedSettings: queued.length ? Object.assign({}, ...queued.map((request) => request.settings)) as ComposerSettingsPatch : null };
}

function settingsMatch(canonical: ComposerSettingsPatch | null | undefined, patch: ComposerSettingsPatch | null): boolean {
  return Boolean(canonical && patch && Object.entries(patch).every(([key, value]) => canonical[key] === value));
}

function errorCode(error: unknown): string | null {
  const code = error && typeof error === "object" && "code" in error ? error.code : null;
  if (typeof code === "string" && code.trim()) return code.trim();
  return error instanceof Error && error.name === "AbortError" ? "aborted" : null;
}

function errorMessage(error: unknown): string | null {
  const message = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  return message.trim() || null;
}

function entryKey(targetKey: string, section: ComposerOptionsSection | undefined): string {
  return `${targetKey}\u0000${section ?? ""}`;
}

function abortReason(signal: AbortSignal): unknown {
  return signal.reason ?? new DOMException("Aborted", "AbortError");
}

/** Two requests with the same signature are interchangeable. */
export function composerOptionsRequestSignature(input: { agentSessionId?: string | null; provider?: string; cwd?: string | null; settings?: ComposerSettingsLike | null }): string {
  const text = (value: string | null | undefined) => value?.trim() || null;
  const settings = input.settings;
  return JSON.stringify({
    agentSessionId: text(input.agentSessionId), provider: input.provider?.trim() ?? "", cwd: input.cwd?.trim() ?? "",
    settings: {
      model: text(settings?.model), reasoningEffort: text(settings?.reasoningEffort), speed: text(settings?.speed),
      planMode: typeof settings?.planMode === "boolean" ? settings.planMode : null, permissionModeId: text(settings?.permissionModeId),
    },
  });
}

/** Each section answers for its own fields, and connectors load apart from the rest of the catalog. */
export function mergeComposerOptionsSection<Options extends ComposerOptionsLike>(existing: Options | undefined, incoming: Options, section: ComposerOptionsSection): Options {
  if (!existing) return incoming;
  if (section === "core") {
    return { ...existing, ...incoming, skills: existing.skills, capabilityCatalog: existing.capabilityCatalog, commands: existing.commands };
  }
  const connector = (capability: { kind?: string }) => capability.kind === "connector";
  const existingCatalog = existing.capabilityCatalog ?? [];
  const incomingCatalog = incoming.capabilityCatalog ?? [];
  if (section === "connectors") {
    return { ...existing, capabilityCatalog: [...existingCatalog.filter((item) => !connector(item)), ...incomingCatalog.filter(connector)], loadedAtUnixMs: incoming.loadedAtUnixMs };
  }
  const incomingConnectors = incomingCatalog.filter(connector);
  return {
    ...existing,
    capabilities: incoming.capabilities ?? existing.capabilities, commands: incoming.commands, skills: incoming.skills,
    capabilityCatalog: [...incomingCatalog.filter((item) => !connector(item)), ...(incomingConnectors.length ? incomingConnectors : existingCatalog.filter(connector))],
    loadedAtUnixMs: incoming.loadedAtUnixMs,
  };
}
