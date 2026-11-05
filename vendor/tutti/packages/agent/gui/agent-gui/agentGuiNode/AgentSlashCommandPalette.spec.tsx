import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AgentSlashCommandPalette } from "./AgentSlashCommandPalette";

describe("AgentSlashCommandPalette", () => {
  it("renders the Side icon for the provider-neutral slash command", () => {
    render(
      <AgentSlashCommandPalette
        label="Slash commands"
        commandsGroupLabel="Commands"
        capabilitiesGroupLabel="Capabilities"
        skillsGroupLabel="Skills"
        pluginsGroupLabel="Plugins"
        connectorsGroupLabel="Connectors"
        connectorConnectedLabel="Connected"
        connectorNotConnectedLabel="Not connected"
        connectorUnsupportedLabel="Unsupported"
        mcpGroupLabel="MCP"
        highlightedIndex={0}
        entries={[
          {
            type: "command",
            key: "command:side",
            label: "side",
            command: {
              name: "side",
              description: "Open a temporary conversation"
            }
          }
        ]}
        onHighlightChange={vi.fn()}
        onSelect={vi.fn()}
        onSelectCapability={vi.fn()}
        onSelectSkill={vi.fn()}
      />
    );

    expect(
      screen.getByRole("option", { name: /side/i }).querySelector("svg")
    ).toHaveClass("lucide-message-circle-plus");
  });

  it("renders a capability section and dispatches capability selection", () => {
    const onSelectCapability = vi.fn();

    render(
      <AgentSlashCommandPalette
        label="Slash commands"
        commandsGroupLabel="Commands"
        capabilitiesGroupLabel="Capabilities"
        skillsGroupLabel="Skills"
        pluginsGroupLabel="Plugins"
        connectorsGroupLabel="Connectors"
        connectorConnectedLabel="Connected"
        connectorNotConnectedLabel="Not connected"
        connectorUnsupportedLabel="Unsupported"
        mcpGroupLabel="MCP"
        highlightedIndex={0}
        entries={[
          {
            type: "capability",
            key: "capability:browserUse",
            label: "Browser",
            description: "Let the agent use a browser.",
            capability: {
              kind: "capability",
              capability: "browserUse",
              name: "browser",
              aliases: ["浏览器"]
            }
          }
        ]}
        onHighlightChange={vi.fn()}
        onSelect={vi.fn()}
        onSelectCapability={onSelectCapability}
        onSelectSkill={vi.fn()}
      />
    );

    expect(screen.getByText("Capabilities")).toBeInTheDocument();
    screen.getByRole("option", { name: /Browser/i }).click();
    expect(onSelectCapability).toHaveBeenCalledWith({
      kind: "capability",
      capability: "browserUse",
      name: "browser",
      aliases: ["浏览器"]
    });
    expect(
      screen.getByRole("option", { name: /Browser/i }).querySelector("svg")
    ).toBeTruthy();
  });

  it("renders inline settings on capability entries and dispatches settings selection", () => {
    const onSelectCapabilitySettings = vi.fn();

    render(
      <AgentSlashCommandPalette
        label="Slash commands"
        commandsGroupLabel="Commands"
        capabilitiesGroupLabel="Capabilities"
        skillsGroupLabel="Skills"
        pluginsGroupLabel="Plugins"
        connectorsGroupLabel="Connectors"
        connectorConnectedLabel="Connected"
        connectorNotConnectedLabel="Not connected"
        connectorUnsupportedLabel="Unsupported"
        mcpGroupLabel="MCP"
        highlightedIndex={0}
        entries={[
          {
            type: "capability",
            key: "capability:computerUse",
            label: "Computer",
            description: "Install or grant access.",
            settingsAriaLabel: "Computer use setup",
            settingsLabel: "Settings",
            capability: {
              kind: "capability",
              capability: "computerUse",
              name: "computer",
              aliases: ["电脑"]
            }
          }
        ]}
        onHighlightChange={vi.fn()}
        onSelect={vi.fn()}
        onSelectCapability={vi.fn()}
        onSelectCapabilitySettings={onSelectCapabilitySettings}
        onSelectSkill={vi.fn()}
      />
    );

    expect(screen.getByText("Capabilities")).toBeInTheDocument();
    screen.getByRole("button", { name: "Computer use setup" }).click();
    expect(onSelectCapabilitySettings).toHaveBeenCalledWith({
      kind: "capability",
      capability: "computerUse",
      name: "computer",
      aliases: ["电脑"]
    });
  });

  it("keeps a read-only capability visible without dispatching mutations", () => {
    const onSelectCapability = vi.fn();
    const onSelectCapabilitySettings = vi.fn();

    render(
      <AgentSlashCommandPalette
        label="Slash commands"
        commandsGroupLabel="Commands"
        capabilitiesGroupLabel="Capabilities"
        skillsGroupLabel="Skills"
        pluginsGroupLabel="Plugins"
        connectorsGroupLabel="Connectors"
        connectorConnectedLabel="Connected"
        connectorNotConnectedLabel="Not connected"
        connectorUnsupportedLabel="Unsupported"
        mcpGroupLabel="MCP"
        highlightedIndex={0}
        entries={[
          {
            type: "capability",
            key: "capability:computerUse",
            label: "Computer",
            disabled: true,
            settingsAriaLabel: "Computer use setup",
            settingsLabel: "Settings",
            capability: {
              kind: "capability",
              capability: "computerUse",
              name: "computer"
            }
          }
        ]}
        onHighlightChange={vi.fn()}
        onSelect={vi.fn()}
        onSelectCapability={onSelectCapability}
        onSelectCapabilitySettings={onSelectCapabilitySettings}
        onSelectSkill={vi.fn()}
      />
    );

    const capability = screen.getByRole("option", { name: /computer/i });
    expect(capability).toHaveAttribute("aria-disabled", "true");
    capability.click();
    screen.getByRole("button", { name: "Computer use setup" }).click();

    expect(onSelectCapability).not.toHaveBeenCalled();
    expect(onSelectCapabilitySettings).not.toHaveBeenCalled();
  });

  it("separates catalog skills, plugins, and connectors into source groups", () => {
    const onSelectSkill = vi.fn();
    render(
      <AgentSlashCommandPalette
        label="Slash commands"
        commandsGroupLabel="Commands"
        capabilitiesGroupLabel="Capabilities"
        skillsGroupLabel="Skills"
        pluginsGroupLabel="Plugins"
        connectorsGroupLabel="Connectors"
        connectorConnectedLabel="Connected"
        connectorNotConnectedLabel="Not connected"
        connectorUnsupportedLabel="Unsupported"
        mcpGroupLabel="MCP"
        highlightedIndex={0}
        entries={[
          {
            type: "skill",
            key: "skill:catalog-review",
            label: "catalog-review",
            skill: {
              name: "catalog-review",
              trigger: "$catalog-review",
              sourceKind: "plugin",
              kind: "skill"
            }
          },
          {
            type: "skill",
            key: "skill:plugin-review",
            label: "plugin-review",
            skill: {
              name: "plugin-review",
              trigger: "$plugin-review",
              sourceKind: "plugin",
              pluginName: "review-tools"
            }
          },
          {
            type: "skill",
            key: "skill:google-drive",
            label: "google-drive",
            skill: {
              name: "Google Drive",
              connectorKey: "google-drive",
              iconUrl: "data:image/png;base64,ZHJpdmU=",
              trigger: "$google-drive",
              sourceKind: "connector",
              kind: "connector",
              status: "available"
            }
          },
          {
            type: "skill",
            key: "skill:notion",
            label: "Notion",
            skill: {
              name: "Notion",
              connectorKey: "notion",
              trigger: "/notion",
              sourceKind: "connector",
              kind: "connector",
              status: "setupRequired"
            }
          }
        ]}
        onHighlightChange={vi.fn()}
        onSelect={vi.fn()}
        onSelectCapability={vi.fn()}
        onSelectSkill={onSelectSkill}
      />
    );

    expect(screen.getByText("Skills")).toBeInTheDocument();
    expect(screen.getByText("Plugins")).toBeInTheDocument();
    expect(screen.getByText("Connectors")).toHaveClass(
      "mt-1",
      "pt-3",
      "before:inset-x-2",
      "before:border-t",
      "before:border-border"
    );
    expect(screen.getByText("Connected")).toHaveClass(
      "text-[var(--state-success)]"
    );
    const googleDriveOption = screen.getByRole("option", {
      name: /google-drive/i
    });
    const googleDriveIcon = googleDriveOption.querySelector("img");
    expect(googleDriveIcon).toHaveAttribute(
      "src",
      "data:image/png;base64,ZHJpdmU="
    );
    expect(googleDriveOption.querySelector("svg")).toBeNull();

    fireEvent.error(googleDriveIcon!);

    expect(googleDriveOption.querySelector("img")).toBeNull();
    expect(googleDriveOption.querySelector("svg")).toBeInTheDocument();
    screen.getByRole("button", { name: "Not connected" }).click();
    expect(onSelectSkill).toHaveBeenCalledWith(
      expect.objectContaining({ connectorKey: "notion" })
    );
  });
});
