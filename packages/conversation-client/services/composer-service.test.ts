// @vitest-environment node
import { autorun } from "mobx";
import { describe, expect, it, vi } from "vitest";
import type { RuntimeModelsResult } from "@multica/core/types";
import { GLOBAL_EXECUTION_PERMISSION_KEY } from "../models/composer-preferences";
import { ComposerService, composerOptionsRequestSignature, mergeComposerOptionsSection, type ComposerOptionsRequest, type ComposerServiceOptions, type ComposerSettingsPatch } from "./composer-service";

type Options = { provider: string; loadedAtUnixMs?: number; models?: string[]; skills?: string[]; commands?: string[]; capabilities?: unknown; capabilityCatalog?: { id: string; kind?: string }[] };
const request = (fields: Partial<ComposerOptionsRequest> = {}): ComposerOptionsRequest => ({ targetKey: "t", provider: "p", ...fields });

const CATALOG_MODELS = ["m", "m2", "m3", "first", "third", "queued", "draft-model"];

/** A conversation "c" on agent "a", with storage held in memory. */
function selections(fields: { session?: Partial<NonNullable<ReturnType<ComposerServiceOptions["session"]>>> | null; stored?: Map<string, string>; busy?: () => boolean; levels?: string[] } = {}) {
  const stored = fields.stored ?? new Map<string, string>();
  const state = { session: fields.session === null ? null : { agentId: "a", runtimeBlocked: false, settings: {} as ComposerSettingsPatch, ...fields.session }, changed: [] as string[] };
  const writes: { key: string; value: string }[] = [];
  let blockWrites: Promise<void> | null = null;
  const options: ComposerServiceOptions = {
    storage: {
      read: async (key) => stored.get(key) ?? null,
      write: async (key, value) => { writes.push({ key, value }); if (blockWrites) await blockWrites; stored.set(key, value); },
    },
    listModels: async () => ({ models: CATALOG_MODELS.map((id) => ({ id, label: id, thinking: { supported_levels: (fields.levels ?? ["high"]).map((value) => ({ value, label: value })) } })), supported: true }) as unknown as RuntimeModelsResult,
    agent: () => ({ provider: "codex", model: "m" }),
    requireAgent: () => ({ model: "m" }),
    assertInvokable: () => ({ model: "m" }),
    assertOpen: () => {},
    conversation: (id) => id === "c" ? { agentId: "a", detail: null } : undefined,
    storedId: (id) => id,
    busy: fields.busy ?? (() => false),
    selectionChanged: (id) => { state.changed.push(id); },
    session: (id) => id === "c" ? state.session : null,
  };
  return { options, state, stored, writes, gateWrites(gate: Promise<void> | null) { blockWrites = gate; } };
}

function deferred() {
  const loads: { resolve(value: Options): void; reject(error: Error): void; request: ComposerOptionsRequest }[] = [];
  const service = new ComposerService<Options>((value) => new Promise((resolve, reject) => { loads.push({ resolve, reject, request: value }); }), selections().options);
  return { loads, service };
}

describe("loading a target's options", () => {
  it("answers a repeat from what settled, and force reads again", async () => {
    const { loads, service } = deferred();
    const first = service.load(request());
    loads[0]!.resolve({ provider: "p", models: ["a"] });
    expect((await first).models).toEqual(["a"]);
    await service.load(request());
    expect(loads).toHaveLength(1);
    void service.load(request({ force: true })).catch(() => {});
    expect(loads).toHaveLength(2);
  });

  it("shares a load under way with a duplicate, not with a different request", () => {
    const { loads, service } = deferred();
    void service.load(request());
    void service.load(request());
    expect(loads).toHaveLength(1);
    void service.load(request({ cwd: "/other" })).catch(() => {});
    expect(loads).toHaveLength(2);
  });

  it("rejects a superseded load, whose late answer changes nothing", async () => {
    const { loads, service } = deferred();
    const older = service.load(request({ cwd: "/a" }));
    const newer = service.load(request({ cwd: "/b" }));
    loads[1]!.resolve({ provider: "p", models: ["new"] });
    await newer;
    loads[0]!.resolve({ provider: "p", models: ["old"] });
    await expect(older).rejects.toThrow("composer_options_load_superseded");
    expect(service.options("t")?.models).toEqual(["new"]);
    expect(service.status("t")).toBe("ready");
  });

  it("reports a failure and keeps what was shown", async () => {
    const { loads, service } = deferred();
    const first = service.load(request());
    loads[0]!.resolve({ provider: "p", models: ["a"] });
    await first;
    const failing = service.load(request({ force: true }));
    expect(service.status("t")).toBe("loading");
    loads[1]!.reject(new Error("boom"));
    await expect(failing).rejects.toThrow("composer_options_load_failed");
    expect(service.status("t")).toBe("error");
    expect(service.options("t")?.models).toEqual(["a"]);
  });

  it("treats an answer that is not options as a failure", async () => {
    const { loads, service } = deferred();
    const load = service.load(request());
    loads[0]!.resolve({} as Options);
    await expect(load).rejects.toThrow("composer_options_load_failed");
  });

  it("does not load an unnamed target", async () => {
    const { loads, service } = deferred();
    await expect(service.load(request({ targetKey: " " }))).rejects.toThrow("composer_options_load_not_accepted");
    expect(loads).toHaveLength(0);
  });

  it("lets one caller give up without stopping the load for others", async () => {
    const { loads, service } = deferred();
    const controller = new AbortController();
    const leaving = service.load(request({ signal: controller.signal }));
    const staying = service.load(request());
    controller.abort(new Error("left"));
    await expect(leaving).rejects.toThrow("left");
    loads[0]!.resolve({ provider: "p", models: ["a"] });
    expect((await staying).models).toEqual(["a"]);
    const aborted = new AbortController();
    aborted.abort(new Error("already"));
    await expect(service.load(request({ signal: aborted.signal }))).rejects.toThrow("already");
  });

  it("reads again after an invalidation, for that provider or target only", async () => {
    const { loads, service } = deferred();
    const one = service.load(request());
    const two = service.load(request({ targetKey: "other", provider: "q" }));
    loads[0]!.resolve({ provider: "p" }); loads[1]!.resolve({ provider: "q" });
    await Promise.all([one, two]);
    service.invalidate({ providers: ["p"] });
    void service.load(request());
    void service.load(request({ targetKey: "other", provider: "q" }));
    expect(loads).toHaveLength(3);
    expect(service.options("t")).not.toBeNull();
  });

  it("tells an observer of one target about that target", async () => {
    const { loads, service } = deferred();
    const seen: (string[] | undefined)[] = [];
    const stop = autorun(() => seen.push(service.options("t")?.models));
    const other = service.load(request({ targetKey: "other" }));
    loads[0]!.resolve({ provider: "p", models: ["x"] });
    await other;
    const own = service.load(request());
    loads[1]!.resolve({ provider: "p", models: ["a"] });
    await own;
    stop();
    expect(seen).toEqual([undefined, ["a"]]);
  });
});

describe("loading a section", () => {
  const base: Options = { provider: "p", models: ["m"], skills: ["s"], commands: ["c"], capabilities: { a: 1 }, capabilityCatalog: [{ id: "tool" }, { id: "conn", kind: "connector" }] };

  it("keeps the other sections' fields when core arrives", () => {
    const merged = mergeComposerOptionsSection(base, { provider: "p", models: ["new"], skills: [], commands: [], capabilityCatalog: [] }, "core");
    expect(merged).toMatchObject({ models: ["new"], skills: ["s"], commands: ["c"], capabilityCatalog: base.capabilityCatalog });
  });

  it("replaces only connectors when they arrive", () => {
    const merged = mergeComposerOptionsSection(base, { provider: "p", models: ["ignored"], capabilityCatalog: [{ id: "next", kind: "connector" }, { id: "ignored" }] }, "connectors");
    expect(merged.capabilityCatalog).toEqual([{ id: "tool" }, { id: "next", kind: "connector" }]);
    expect(merged.models).toEqual(["m"]);
  });

  it("keeps known connectors when capabilities arrive without any", () => {
    const merged = mergeComposerOptionsSection(base, { provider: "p", skills: ["n"], commands: [], capabilityCatalog: [{ id: "fresh" }] }, "capabilities");
    expect(merged).toMatchObject({ skills: ["n"], models: ["m"] });
    expect(merged.capabilityCatalog).toEqual([{ id: "fresh" }, { id: "conn", kind: "connector" }]);
  });

  it("reports the core section as the target's state too", async () => {
    const { loads, service } = deferred();
    const load = service.load(request({ section: "core" }));
    expect(service.status("t")).toBe("loading");
    expect(service.status("t", "core")).toBe("loading");
    expect(service.status("t", "connectors")).toBeUndefined();
    loads[0]!.resolve({ provider: "p" });
    await load;
    expect(service.status("t")).toBe("ready");
  });
});

describe("a request's signature", () => {
  it("ignores spacing and an empty setting, and not a real difference", () => {
    const signature = composerOptionsRequestSignature;
    expect(signature({ provider: " p ", cwd: " /a " })).toBe(signature({ provider: "p", cwd: "/a", settings: { model: "  " } }));
    expect(signature({ provider: "p", settings: { model: "a" } })).not.toBe(signature({ provider: "p", settings: { model: "b" } }));
    expect(vi.isMockFunction(signature)).toBe(false);
  });
});

function settingsHarness(fields: Parameters<typeof selections>[0] = {}) {
  const harness = selections(fields);
  const service = new ComposerService<Options>(async () => ({ provider: "p" }), harness.options);
  return { ...harness, service };
}
const flush = async () => { for (let index = 0; index < 20; index++) await Promise.resolve(); };
const status = (service: ComposerService<Options>) => service.settingsUpdate("c")?.status ?? null;

describe("writing a conversation's settings", () => {
  it("trims the conversation id, shows the write while it runs and clears when it lands", async () => {
    const { service, writes } = settingsHarness();
    service.updateSettings({ agentSessionId: " c ", settings: { model: "m2" } });
    expect(service.settingsUpdate("c")).toMatchObject({ status: "inFlight", settings: { model: "m2" }, queuedSettings: null });
    await flush();
    expect(status(service)).toBeNull();
    expect(writes.map((write) => write.key)).toEqual(["model-preferences-v1"]);
    expect(service.selectedModel("a", "c")).toBe("m2");
  });

  it("notifies observers as the write moves on", async () => {
    const { service } = settingsHarness();
    const seen: (string | null)[] = [];
    const stop = autorun(() => { seen.push(status(service)); });
    service.updateSettings({ agentSessionId: "c", settings: { model: "m2" } });
    await flush();
    stop();
    expect(seen).toEqual([null, "inFlight", null]);
  });

  it("ignores an empty patch, and a conversation it does not have", () => {
    const { service } = settingsHarness();
    service.updateSettings({ agentSessionId: "c", settings: {} });
    service.updateSettings({ agentSessionId: "other", settings: { model: "m2" } });
    expect(service.settingsUpdate("c")).toBeNull();
    expect(service.settingsUpdate("other")).toBeNull();
  });

  it("merges user choices behind a running write, then writes them together", async () => {
    const { service, writes, gateWrites } = settingsHarness();
    let release!: () => void;
    gateWrites(new Promise((resolve) => { release = resolve; }));
    service.updateSettings({ agentSessionId: "c", settings: { model: "first" } });
    await flush();
    service.updateSettings({ agentSessionId: "c", settings: { model: "second" } });
    service.updateSettings({ agentSessionId: "c", settings: { model: "third", reasoningEffort: "high" } });
    expect(service.settingsUpdate("c")).toMatchObject({ settings: { model: "first" }, queuedSettings: { model: "third", reasoningEffort: "high" } });
    gateWrites(null); release();
    await flush();
    expect(writes.map((write) => write.key)).toEqual(["model-preferences-v1", "model-preferences-v1", "reasoning-preferences-v1"]);
    expect(writes[1]!.value).toContain("third");
    expect(writes.some((write) => write.value.includes("second"))).toBe(false);
    expect(status(service)).toBeNull();
  });

  it("times out after 30 seconds as unknown, and a late answer changes nothing", async () => {
    vi.useFakeTimers();
    try {
      const { service, gateWrites } = settingsHarness();
      let release!: () => void;
      gateWrites(new Promise((resolve) => { release = resolve; }));
      service.updateSettings({ agentSessionId: "c", settings: { model: "m2" } });
      await vi.advanceTimersByTimeAsync(29_999);
      expect(status(service)).toBe("inFlight");
      await vi.advanceTimersByTimeAsync(1);
      const unknown = service.settingsUpdate("c");
      expect(unknown?.status).toBe("unknown");
      gateWrites(null); release();
      await vi.advanceTimersByTimeAsync(0);
      expect(service.settingsUpdate("c")).toBe(unknown);
    } finally { vi.useRealTimers(); }
  });

  it("treats the next choice after an unknown outcome as the retry, carrying what was in doubt", async () => {
    vi.useFakeTimers();
    try {
      const { service, gateWrites, writes } = settingsHarness();
      let release!: () => void;
      gateWrites(new Promise((resolve) => { release = resolve; }));
      service.updateSettings({ agentSessionId: "c", settings: { model: "m2" } });
      await vi.advanceTimersByTimeAsync(1);
      service.updateSettings({ agentSessionId: "c", settings: { reasoningEffort: "high" } });
      await vi.advanceTimersByTimeAsync(30_000);
      expect(service.settingsUpdate("c")).toMatchObject({ status: "unknown", queuedSettings: { reasoningEffort: "high" } });
      service.updateSettings({ agentSessionId: "c", settings: { model: "m3" } });
      expect(service.settingsUpdate("c")).toMatchObject({ status: "inFlight", settings: { model: "m3", reasoningEffort: "high" }, queuedSettings: null });
      gateWrites(null); release();
      await vi.advanceTimersByTimeAsync(0);
      expect(status(service)).toBeNull();
      expect(writes.at(-1)!.key).toBe("reasoning-preferences-v1");
    } finally { vi.useRealTimers(); }
  });

  it("clears an unknown outcome once the conversation shows those settings", async () => {
    vi.useFakeTimers();
    try {
      const { service, gateWrites, state } = settingsHarness();
      gateWrites(new Promise(() => {}));
      service.updateSettings({ agentSessionId: "c", settings: { model: "m2" } });
      await vi.advanceTimersByTimeAsync(30_000);
      service.reconcileSettings();
      expect(status(service)).toBe("unknown");
      state.session!.settings = { model: "m2" };
      service.reconcileSettings();
      expect(status(service)).toBeNull();
    } finally { vi.useRealTimers(); }
  });

  it("reports a failure, which a new choice replaces", async () => {
    const { service } = settingsHarness();
    service.updateSettings({ agentSessionId: "c", settings: { planMode: true } });
    await flush();
    expect(service.settingsUpdate("c")).toMatchObject({ status: "failed", errorCode: "unsupported", settings: { planMode: true } });
    service.updateSettings({ agentSessionId: "c", settings: { model: "m2" } });
    expect(service.settingsUpdate("c")).toMatchObject({ status: "inFlight", settings: { model: "m2" }, errorCode: null });
    await flush();
    expect(status(service)).toBeNull();
  });

  it("runs what was queued behind a failure with the next choice", async () => {
    const { service, writes, gateWrites } = settingsHarness();
    let release!: () => void;
    gateWrites(new Promise((resolve) => { release = resolve; }));
    service.updateSettings({ agentSessionId: "c", settings: { planMode: true } });
    service.updateSettings({ agentSessionId: "c", settings: { model: "queued" } });
    gateWrites(null); release();
    await flush();
    expect(service.settingsUpdate("c")).toMatchObject({ status: "failed", queuedSettings: { model: "queued" } });
    service.updateSettings({ agentSessionId: "c", settings: { reasoningEffort: "high" } });
    expect(service.settingsUpdate("c")).toMatchObject({ status: "inFlight", settings: { model: "queued", reasoningEffort: "high" } });
    await flush();
    expect(writes.map((write) => write.key)).toEqual(["model-preferences-v1", "reasoning-preferences-v1"]);
  });

  it("rejects a user's choice while the runtime is blocked", () => {
    const { service } = settingsHarness({ session: { runtimeBlocked: true } });
    service.updateSettings({ agentSessionId: "c", settings: { model: "m2" } });
    expect(service.settingsUpdate("c")).toBeNull();
  });

  it("holds a queued write while the runtime is away and sends it on return", async () => {
    const { service, state, writes, gateWrites } = settingsHarness();
    let release!: () => void;
    gateWrites(new Promise((resolve) => { release = resolve; }));
    service.updateSettings({ agentSessionId: "c", settings: { model: "first" } });
    await flush();
    service.updateSettings({ agentSessionId: "c", settings: { reasoningEffort: "high" } });
    state.session!.runtimeBlocked = true;
    gateWrites(null); release();
    await flush();
    expect(service.settingsUpdate("c")).toMatchObject({ status: "waitingForRuntime", settings: { reasoningEffort: "high" } });
    expect(writes.map((write) => write.key)).toEqual(["model-preferences-v1"]);
    state.session!.runtimeBlocked = false;
    service.reconcileSettings();
    expect(status(service)).toBe("inFlight");
    await flush();
    expect(status(service)).toBeNull();
    expect(writes.map((write) => write.key)).toEqual(["model-preferences-v1", "reasoning-preferences-v1"]);
  });

  it("keeps a new conversation's patch in line without merging it with a user's choices", async () => {
    const { service, writes, gateWrites } = settingsHarness();
    let release!: () => void;
    gateWrites(new Promise((resolve) => { release = resolve; }));
    service.updateSettings({ agentSessionId: "c", settings: { model: "first" } });
    await flush();
    const activation = service.applyActivationSettings("c", { model: "from-activation" });
    service.updateSettings({ agentSessionId: "c", settings: { model: "after" } });
    expect(service.settingsUpdate("c")?.queuedSettings).toEqual({ model: "after" });
    gateWrites(null); release();
    await activation;
    await flush();
    expect(writes.map((write) => JSON.parse(write.value)[0][1])).toEqual(["first", "from-activation", "after"]);
    expect(status(service)).toBeNull();
  });

  it("answers a new conversation's patch with its outcome, and has no timeout", async () => {
    vi.useFakeTimers();
    try {
      const { service, gateWrites } = settingsHarness();
      let release!: () => void;
      gateWrites(new Promise((resolve) => { release = resolve; }));
      const applying = service.applyActivationSettings("c", { model: "m2" });
      await vi.advanceTimersByTimeAsync(60_000);
      expect(status(service)).toBe("inFlight");
      gateWrites(null); release();
      await expect(applying).resolves.toBeUndefined();
      await expect(service.applyActivationSettings("c", { planMode: true })).rejects.toMatchObject({ code: "unsupported" });
      expect(status(service)).toBe("failed");
      await expect(service.applyActivationSettings("other", { model: "m2" })).rejects.toMatchObject({ code: "session_not_loaded" });
    } finally { vi.useRealTimers(); }
  });
});

describe("choices", () => {
  it("reads saved choices and refuses a change during a run", async () => {
    let busy = false;
    const { service } = settingsHarness({ stored: new Map([["model-preferences-v1", JSON.stringify([["session:c", "saved"]])]]), busy: () => busy });
    await service.hydrated();
    expect(service.selectedModel("a", "c")).toBe("saved");
    busy = true;
    await expect(service.setModelSelection("a", "c", "other")).rejects.toMatchObject({ code: "run_in_progress", status: 409 });
    expect(service.selectedModel("a", "c")).toBe("saved");
  });

  it("keeps the old model when saving fails", async () => {
    const harness = selections();
    harness.options.storage.write = async () => { throw new Error("storage full"); };
    const service = new ComposerService<Options>(async () => ({ provider: "p" }), harness.options);
    await service.setModelSelection("a", null, "first").catch(() => {});
    await expect(service.setModelSelection("a", null, "second")).rejects.toThrow("storage full");
    expect(service.selectedModel("a")).toBeNull();
  });

  it("refuses a thinking effort the model does not offer", async () => {
    const { service } = settingsHarness({ levels: ["low"] });
    await expect(service.setReasoningSelection("a", null, "high")).rejects.toMatchObject({ code: "invalid_reasoning_effort" });
    await service.setReasoningSelection("a", null, "low");
    expect(service.selectedReasoning("a")).toBe("low");
  });

  it("changes a conversation's choice only for its own agent, and tells the conversation", async () => {
    const { service, state } = settingsHarness();
    await expect(service.setModelSelection("b", "c", "m2")).rejects.toMatchObject({ code: "invalid_session" });
    await service.setModelSelection("a", "c", "m2");
    expect(state.changed).toEqual(["c"]);
    expect(service.selectedModel("b", "c")).toBeNull();
  });

  it("shares one permission across providers and existing conversations and restores it after restart", async () => {
    const { service, stored } = settingsHarness();
    expect(service.selectedPermission("a")).toBe("default");
    await service.setPermissionSelection("a", "c", "full-access");
    expect(service.selectedPermission("a")).toBe("full-access");
    expect(service.selectedPermission("a", "c")).toBe("full-access");
    expect(service.selectedPermission("b")).toBe("full-access");
    expect(service.selectedPermission("b", "another-conversation")).toBe("full-access");
    expect(service.permissionRevision).toBe(1);
    const restarted = settingsHarness({ stored }).service;
    await restarted.hydrated();
    expect(restarted.selectedPermission("b", "another-conversation")).toBe("full-access");
    restarted.dispose();
    service.dispose();
  });

  it("updates mounted services when the shared permission changes and unsubscribes on disposal", async () => {
    const { options, stored } = selections();
    const listeners = new Set<(value: string | null) => void>();
    options.storage.subscribe = (key, listener) => {
      expect(key).toBe(GLOBAL_EXECUTION_PERMISSION_KEY);
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    };
    options.storage.write = async (key, value) => {
      stored.set(key, value);
      for (const listener of listeners) listener(value);
    };
    const first = new ComposerService(async () => ({ provider: "p" }), options);
    const second = new ComposerService(async () => ({ provider: "p" }), options);
    await Promise.all([first.hydrated(), second.hydrated()]);
    const values: string[] = [];
    const stop = autorun(() => { values.push(second.selectedPermission("b", "other")); });
    await first.setPermissionSelection("a", "c", "auto");
    expect(values).toEqual(["default", "auto"]);
    await second.setPermissionSelection("b", null, "default");
    expect(first.selectedPermission("a", "c")).toBe("default");
    stop();
    first.dispose();
    second.dispose();
    expect(listeners.size).toBe(0);
  });

  it.each([null, "broken", '"unknown"', '[["session:c","full-access"]]'])("uses the default for an absent or malformed global permission: %s", async (value) => {
    const stored = new Map(value === null ? [] : [[GLOBAL_EXECUTION_PERMISSION_KEY, value]]);
    const { service } = settingsHarness({ stored });
    await service.hydrated();
    expect(service.selectedPermission("a", "c")).toBe("default");
    service.dispose();
  });

  it.each(["auto", "default"])("does not overwrite a %s change with a late initial storage read", async (mode) => {
    const { options } = selections();
    let receive!: (value: string | null) => void;
    let finishRead!: (value: string | null) => void;
    options.storage.subscribe = (_key, listener) => { receive = listener; return () => {}; };
    options.storage.read = async (key) => key === GLOBAL_EXECUTION_PERMISSION_KEY
      ? new Promise((resolve) => { finishRead = resolve; }) : null;
    const service = new ComposerService(async () => ({ provider: "p" }), options);
    receive(JSON.stringify(mode));
    finishRead('"full-access"');
    await service.hydrated();
    expect(service.selectedPermission("a", "c")).toBe(mode);
    service.dispose();
  });

  it("seeds a new conversation from the draft's choices", async () => {
    const { service, stored } = settingsHarness();
    await service.setModelSelection("a", null, "draft-model");
    await service.seedConversation({ id: "a", model: "m" }, "c", { reasoningEffort: "high" });
    expect(service.selectedModel("a", "c")).toBe("draft-model");
    expect(JSON.parse(stored.get("reasoning-preferences-v1")!)).toEqual([["session:c", { model: "draft-model", effort: "high" }]]);
    expect(stored.has(GLOBAL_EXECUTION_PERMISSION_KEY)).toBe(false);
  });
});
