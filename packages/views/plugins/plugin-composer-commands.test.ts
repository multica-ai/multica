// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { PluginComposerCommand, PluginInstallation } from "@multica/core/types/plugin";
import { collectComposerCommands } from "./plugin-composer-commands";

function installation(overrides: Partial<PluginInstallation> = {}): PluginInstallation {
  return {
    id: "installation-1",
    plugin_key: "com.example.plugin",
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

function surface(key: string, type = "modal", platforms?: string[]) {
  return { key, type, name: key, entry: "ui/main.js", platforms };
}

function command(key: string, overrides: Partial<PluginComposerCommand> = {}): PluginComposerCommand {
  return {
    key,
    label: key,
    contexts: ["chat", "issue_comment"],
    surface: "command-dialog",
    ...overrides,
  };
}

describe("collectComposerCommands", () => {
  it("requires an enabled installation and a command declared for this context", () => {
    const result = collectComposerCommands([
      installation({
        composer_commands: [command("chat-only")],
        surfaces: [surface("command-dialog")],
      }),
      installation({
        id: "disabled",
        enabled: false,
        composer_commands: [command("disabled")],
        surfaces: [surface("command-dialog")],
      }),
    ], "issue_reply", "web");

    expect(result).toEqual([]);
  });

  it("refuses an installation without an immutable package version", () => {
    const result = collectComposerCommands([installation({
      package_version_id: "",
      composer_commands: [command("skills")],
      surfaces: [surface("command-dialog")],
    })], "chat", "web");

    expect(result).toEqual([]);
  });

  it("requires one matching modal surface and rejects missing, ambiguous, or non-modal targets", () => {
    const result = collectComposerCommands([
      installation({
        composer_commands: [
          command("missing", { surface: "missing" }),
          command("panel", { surface: "panel" }),
          command("ambiguous", { surface: "duplicate" }),
        ],
        surfaces: [
          surface("panel", "issue_panel"),
          surface("duplicate"),
          surface("duplicate"),
        ],
      }),
    ], "chat", "web");

    expect(result).toEqual([]);
  });

  it("filters restricted surfaces by platform and fails closed when platform is unknown", () => {
    const plugins = [installation({
      composer_commands: [command("desktop"), command("universal", { surface: "universal" })],
      surfaces: [surface("command-dialog", "modal", ["desktop"]), surface("universal")],
    })];

    expect(collectComposerCommands(plugins, "chat", "desktop").map((target) => target.command.key))
      .toEqual(["desktop", "universal"]);
    expect(collectComposerCommands(plugins, "chat", "web").map((target) => target.command.key))
      .toEqual(["universal"]);
    expect(collectComposerCommands(plugins, "chat").map((target) => target.command.key))
      .toEqual(["universal"]);
  });

  it("uses installation-scoped IDs, stable ordering, and deterministic deduplication", () => {
    const first = installation({
      id: "install-b",
      composer_commands: [command("zeta"), command("alpha")],
      surfaces: [surface("command-dialog")],
    });
    const second = installation({
      id: "install-a",
      plugin_key: "com.example.other",
      composer_commands: [command("alpha")],
      surfaces: [surface("command-dialog")],
    });
    const duplicate = installation({
      id: "install-b",
      composer_commands: [command("zeta", { label: "alternate duplicate" })],
      surfaces: [surface("command-dialog")],
    });

    const targets = collectComposerCommands([first, duplicate, second], "chat", "web");

    expect(targets.map((target) => target.id)).toEqual([
      "install-a:alpha",
      "install-b:alpha",
      "install-b:zeta",
    ]);
    expect(targets[2]?.command.label).toBe("alternate duplicate");
  });
});
