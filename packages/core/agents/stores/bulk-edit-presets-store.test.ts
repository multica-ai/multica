// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { setCurrentWorkspace } from "../../platform/workspace-storage";
import { useAgentBulkEditPresetsStore } from "./bulk-edit-presets-store";

const flush = () => new Promise((resolve) => queueMicrotask(() => resolve(null)));

beforeAll(() => {
  if (typeof globalThis.localStorage?.clear !== "function") {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() { return values.size; },
      clear: () => values.clear(),
      getItem: (k) => values.get(k) ?? null,
      key: (i) => Array.from(values.keys())[i] ?? null,
      removeItem: (k) => { values.delete(k); },
      setItem: (k, v) => { values.set(k, v); },
    };
    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: storage });
    Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  }
});

beforeEach(() => {
  localStorage.clear();
  setCurrentWorkspace(null, null);
  useAgentBulkEditPresetsStore.setState({ presets: [] });
});

afterEach(() => {
  setCurrentWorkspace(null, null);
});

describe("useAgentBulkEditPresetsStore", () => {
  it("persists bulk edit presets under the workspace namespace", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();

    useAgentBulkEditPresetsStore.getState().savePreset("Claude local", {
      model: "claude-sonnet-4",
      customArgsPatch: [
        { action: "add", value: "--permission-mode" },
        { action: "add", value: "acceptEdits" },
      ],
      env: [{ action: "remove", key: "OLD_KEY" }],
    });

    const raw = localStorage.getItem("multica_agent_bulk_edit_presets:acme");
    expect(raw).not.toBeNull();
    expect(JSON.parse(raw as string).state.presets).toHaveLength(1);
  });

  it("does not persist env values in local presets", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();

    useAgentBulkEditPresetsStore.getState().savePreset("Secret skeleton", {
      env: [
        { action: "set", key: "ANTHROPIC_API_KEY", value: "sk-secret" },
        { action: "remove", key: "OLD_KEY" },
      ] as never,
    });

    const preset = useAgentBulkEditPresetsStore.getState().presets[0]!;
    expect(preset.patch.env).toEqual([
      { action: "set", key: "ANTHROPIC_API_KEY" },
      { action: "remove", key: "OLD_KEY" },
    ]);
    expect(localStorage.getItem("multica_agent_bulk_edit_presets:acme")).not.toContain("sk-secret");
  });

  it("sanitizes custom arg and env operations before persisting a preset", async () => {
    setCurrentWorkspace("acme", "ws_a");
    await flush();

    useAgentBulkEditPresetsStore.getState().savePreset("  Clean preset  ", {
      customArgsPatch: [
        { action: "add", value: " --verbose " },
        { action: "replace", value: " --old ", replacement: " --new " },
        { action: "replace", value: "--missing-replacement", replacement: " " },
        { action: "remove", value: " " },
      ],
      env: [
        { action: "set", key: " API_KEY " },
        { action: "remove", key: " OLD_KEY " },
        { action: "set", key: " " },
      ],
    } as never);

    const preset = useAgentBulkEditPresetsStore.getState().presets[0]!;
    expect(preset.name).toBe("Clean preset");
    expect(preset.patch.customArgsPatch).toEqual([
      { action: "add", value: "--verbose", replacement: undefined },
      { action: "replace", value: "--old", replacement: "--new" },
    ]);
    expect(preset.patch.env).toEqual([
      { action: "set", key: "API_KEY" },
      { action: "remove", key: "OLD_KEY" },
    ]);
  });
});
