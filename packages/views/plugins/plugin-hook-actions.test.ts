// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { PluginInstallation } from "@multica/core/types";
import { collectManualHookActions, pluginHookActionKey } from "./plugin-hook-actions";

// The canonical matrix for which hooks become menu entries. The component
// suite covers rendering and wiring; it does not re-run these cases.

function installation(overrides: Partial<PluginInstallation> = {}): PluginInstallation {
  return {
    id: "install-1",
    plugin_key: "com.example.one",
    name: "Example",
    version: "1.0.0",
    package_version_id: "version-1",
    enabled: true,
    granted_scopes: [],
    config_schema: [],
    config: {},
    configured_secrets: [],
    surfaces: [],
    hooks: [],
    resources: [],
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

function hook(key: string, triggers: string[]) {
  return { key, name: key, description: "", triggers, transport: "http" };
}

describe("collectManualHookActions", () => {
  it("takes only hooks that declared the manual trigger", () => {
    const actions = collectManualHookActions([
      installation({
        hooks: [
          hook("manual_one", ["manual"]),
          hook("ui_only", ["ui"]),
          hook("event_only", ["event"]),
          hook("agent_only", ["agent"]),
          hook("manual_and_ui", ["ui", "manual"]),
        ],
      }),
    ]);
    expect(actions.map((a) => a.hook.key)).toEqual(["manual_one", "manual_and_ui"]);
  });

  it("skips a disabled installation entirely", () => {
    const actions = collectManualHookActions([
      installation({ enabled: false, hooks: [hook("manual_one", ["manual"])] }),
    ]);
    expect(actions).toEqual([]);
  });

  // Per the API compatibility rule: a backend that stops sending `enabled` must
  // read as off. A truthy check would turn a dropped field into a live menu
  // entry for a plugin the admin had switched off.
  it("treats a missing enabled flag as off rather than truthy", () => {
    const withoutFlag = installation({ hooks: [hook("manual_one", ["manual"])] });
    delete (withoutFlag as { enabled?: boolean }).enabled;
    expect(collectManualHookActions([withoutFlag])).toEqual([]);
  });

  it("survives an installation whose hooks or triggers are missing", () => {
    const noHooks = installation();
    delete (noHooks as { hooks?: unknown }).hooks;
    const noTriggers = installation({
      id: "install-2",
      hooks: [{ key: "k", name: "k", description: "", transport: "http" } as never],
    });
    expect(collectManualHookActions([noHooks, noTriggers])).toEqual([]);
  });

  it("keeps entries from different installations distinct", () => {
    const actions = collectManualHookActions([
      installation({ id: "a", name: "Alpha", hooks: [hook("summarize", ["manual"])] }),
      installation({ id: "b", name: "Beta", hooks: [hook("summarize", ["manual"])] }),
    ]);
    // Same hook key from two plugins: the key has to disambiguate, or React
    // collapses them and only one is clickable.
    const keys = actions.map(pluginHookActionKey);
    expect(new Set(keys).size).toBe(2);
    expect(actions.map((a) => a.installation.name)).toEqual(["Alpha", "Beta"]);
  });
});
