// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { PluginInstallation } from "@multica/core/types";
import { collectModalSurfaces, pluginModalKey } from "./plugin-modal-surface";

// Canonical matrix for which surfaces become modal menu entries. The component
// suite covers rendering; it does not re-run these cases.

function installation(overrides: Partial<PluginInstallation> = {}): PluginInstallation {
  return {
    id: "install-1",
    plugin_key: "com.example.one",
    name: "Example",
    version: "1.0.0",
    source_url: "https://example.com/multica.plugin.json",
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

function surface(key: string, type: string) {
  return { key, type, name: key, entry: "ui/main.js" };
}

describe("collectModalSurfaces", () => {
  it("takes only modal surfaces, leaving panels to their own mount point", () => {
    const targets = collectModalSurfaces([
      installation({
        surfaces: [surface("panel", "issue_panel"), surface("dialog", "modal"), surface("side", "sidebar_panel")],
      }),
    ]);
    expect(targets.map((t) => t.surface.key)).toEqual(["dialog"]);
  });

  it("skips a disabled installation", () => {
    expect(collectModalSurfaces([
      installation({ enabled: false, surfaces: [surface("dialog", "modal")] }),
    ])).toEqual([]);
  });

  // A dropped `enabled` field must read as off. Opening a third party's UI on a
  // truthy check is the wrong direction to fail in.
  it("treats a missing enabled flag as off", () => {
    const withoutFlag = installation({ surfaces: [surface("dialog", "modal")] });
    delete (withoutFlag as { enabled?: boolean }).enabled;
    expect(collectModalSurfaces([withoutFlag])).toEqual([]);
  });

  it("keeps same-named surfaces from different plugins distinct", () => {
    const targets = collectModalSurfaces([
      installation({ id: "a", surfaces: [surface("dialog", "modal")] }),
      installation({ id: "b", surfaces: [surface("dialog", "modal")] }),
    ]);
    expect(new Set(targets.map(pluginModalKey)).size).toBe(2);
  });

  it("survives an installation with no surfaces field", () => {
    const bare = installation();
    delete (bare as { surfaces?: unknown }).surfaces;
    expect(collectModalSurfaces([bare])).toEqual([]);
  });
});
