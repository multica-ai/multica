import { afterEach, describe, expect, it } from "vitest";
import { useHotkeyRegistry } from "./registry";
import type { HotkeyCommand } from "./registry";

function makeCmd(overrides: Partial<HotkeyCommand> = {}): HotkeyCommand {
  return {
    id: "test.cmd",
    keys: "Mod+K",
    description: "Test command",
    scope: "global",
    ...overrides,
  };
}

afterEach(() => {
  // Reset store between tests.
  useHotkeyRegistry.setState({
    commands: new Map(),
    activeScopes: new Set(["global"]),
  });
});

describe("registry", () => {
  it("registers and unregisters commands", () => {
    const { register, unregister } = useHotkeyRegistry.getState();
    register(makeCmd({ id: "a" }));
    register(makeCmd({ id: "b" }));
    expect(useHotkeyRegistry.getState().commands.size).toBe(2);

    unregister("a");
    expect(useHotkeyRegistry.getState().commands.size).toBe(1);
    expect(useHotkeyRegistry.getState().commands.has("b")).toBe(true);
  });

  it("de-duplicates by id (last write wins)", () => {
    const { register } = useHotkeyRegistry.getState();
    register(makeCmd({ id: "x", description: "first" }));
    register(makeCmd({ id: "x", description: "second" }));
    expect(useHotkeyRegistry.getState().commands.size).toBe(1);
    expect(useHotkeyRegistry.getState().commands.get("x")!.description).toBe(
      "second",
    );
  });

  it("filters commands by active scope", () => {
    const { register, activateScope } = useHotkeyRegistry.getState();
    register(makeCmd({ id: "g", scope: "global" }));
    register(makeCmd({ id: "e", scope: "editor" }));
    register(makeCmd({ id: "m", scope: "modal" }));

    // Only global is active by default.
    const { commands, activeScopes } = useHotkeyRegistry.getState();
    const active = [...commands.values()].filter((c) =>
      activeScopes.has(c.scope),
    );
    expect(active.map((c) => c.id)).toEqual(["g"]);

    // Activate editor scope.
    activateScope("editor");
    const state2 = useHotkeyRegistry.getState();
    const active2 = [...state2.commands.values()].filter((c) =>
      state2.activeScopes.has(c.scope),
    );
    expect(active2.map((c) => c.id).sort()).toEqual(["e", "g"]);
  });

  it("never deactivates global scope", () => {
    const { deactivateScope } = useHotkeyRegistry.getState();
    deactivateScope("global");
    expect(useHotkeyRegistry.getState().activeScopes.has("global")).toBe(true);
  });

  it("setActiveScopes replaces all but keeps global", () => {
    const { setActiveScopes } = useHotkeyRegistry.getState();
    setActiveScopes(["editor", "modal"]);
    const scopes = useHotkeyRegistry.getState().activeScopes;
    expect(scopes.has("global")).toBe(true);
    expect(scopes.has("editor")).toBe(true);
    expect(scopes.has("modal")).toBe(true);
    expect(scopes.size).toBe(3);
  });
});
