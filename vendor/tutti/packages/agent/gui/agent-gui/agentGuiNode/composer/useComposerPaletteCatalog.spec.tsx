import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { translateInUiLanguage } from "../../../i18n/index";
import type { AgentGUIComposerSettingsVM } from "../model/agentGuiNodeTypes";
import { resolveSlashCommandSubmitEffect } from "../model/agentSlashCommandProviderPolicy";
import type { AgentComposerRenderProps as AgentComposerProps } from "./AgentComposer.types";
import { agentSlashPaletteLabels } from "./agentSlashPaletteLabels";
import { useComposerPaletteCatalog } from "./useComposerPaletteCatalog";

describe("useComposerPaletteCatalog", () => {
  it("uses localized presentation for declared commands while preserving their identity", () => {
    const commands = ["help", "mcp", "tasks"].map((name) => ({ name }));
    const { result } = renderHook(() =>
      useComposerPaletteCatalog({
        provider: "codex",
        paletteDraftPrompt: "/",
        availableCommands: commands,
        availableSkills: [],
        hasCompactableContext: false,
        compactSupported: false,
        composerSettings: {
          supportsPlanMode: false,
          supportsBrowser: false,
          supportsComputerUse: false,
          slashCommandPolicy: {
            fallbackCommands: [],
            commandEffects: commands.map(({ name }) => ({
              command: name,
              effect: "submitImmediate" as const
            })),
            commandCatalogAuthoritative: true
          }
        } as unknown as AgentGUIComposerSettingsVM,
        capabilityControlsReadOnly: false,
        labels: agentSlashPaletteLabels((key, options) =>
          translateInUiLanguage("zh-CN", key, options)
        ) as AgentComposerProps["labels"],
        uiLanguage: "zh-CN",
        editorHandleRef: { current: null }
      })
    );

    expect(
      result.current.slashPaletteEntries
        .filter((entry) => entry.type === "command")
        .map((entry) => ({
          command: entry.command.name,
          label: entry.primaryLabel,
          rawLabel: entry.secondaryLabel,
          description: entry.description
        }))
    ).toEqual([
      {
        command: "help",
        label: "帮助",
        rawLabel: "help",
        description: "查看可用命令和帮助"
      },
      {
        command: "mcp",
        label: "MCP 服务",
        rawLabel: "mcp",
        description: "管理 MCP 服务"
      },
      {
        command: "tasks",
        label: "任务",
        rawLabel: "tasks",
        description: "查看和管理后台任务"
      }
    ]);
  });

  it("uses fresh host readiness when Composer capability state is stale", () => {
    const { result } = renderHook(() =>
      useComposerPaletteCatalog({
        provider: "codex",
        paletteDraftPrompt: "/",
        availableCommands: [],
        availableSkills: [],
        hasCompactableContext: false,
        compactSupported: false,
        composerSettings: {
          supportsPlanMode: false,
          supportsBrowser: false,
          supportsComputerUse: false,
          slashCommandPolicy: {
            fallbackCommands: [],
            commandEffects: [],
            commandCatalogAuthoritative: true
          }
        } as unknown as AgentGUIComposerSettingsVM,
        capabilityMenuState: {
          computerUse: {
            authorization: "authorized",
            installed: true,
            presentationSupported: true
          }
        },
        capabilityControlsReadOnly: false,
        labels: {
          computerUseCapabilityDescription: "Control the computer",
          computerUseCapabilityLabel: "Computer use",
          computerUseCapabilitySettingsLabel: "Computer use settings",
          capabilityInlineSettingsLabel: "Settings"
        } as AgentComposerProps["labels"],
        uiLanguage: "en",
        editorHandleRef: { current: null }
      })
    );

    expect(result.current.availableCapabilities).toEqual([
      {
        capability: "computerUse",
        label: "Computer use",
        name: "computer",
        trigger: "/computer"
      }
    ]);
    expect(
      result.current.slashPaletteEntries.find(
        (entry) => entry.key === "capability:computerUse"
      )
    ).toMatchObject({ selectAction: "capability", type: "capability" });
    expect(
      resolveSlashCommandSubmitEffect({
        provider: "codex",
        policy: result.current.slashCommandPolicy,
        computerSupported: result.current.computerExecutable,
        commands: result.current.resolvedSlashCommands,
        draft: "/computer click Confirm"
      })
    ).toMatchObject({
      kind: "submitPrompt",
      displayPrompt: "/computer click Confirm",
      requiredSettingsPatch: { computerUse: true }
    });
  });

  it("places connector entries before ordinary skills", () => {
    const { result } = renderHook(() =>
      useComposerPaletteCatalog({
        provider: "codex",
        paletteDraftPrompt: "/",
        availableCommands: [],
        availableSkills: [
          {
            name: "review",
            trigger: "$review",
            sourceKind: "project",
            kind: "skill"
          },
          {
            name: "GitHub",
            connectorKey: "github",
            iconUrl: "data:image/png;base64,Z2l0aHVi",
            trigger: "/github",
            sourceKind: "connector",
            kind: "connector",
            status: "available"
          }
        ],
        hasCompactableContext: false,
        compactSupported: false,
        composerSettings: {
          supportsPlanMode: false,
          supportsBrowser: false,
          supportsComputerUse: false,
          slashCommandPolicy: {
            fallbackCommands: [],
            commandEffects: [],
            commandCatalogAuthoritative: true
          }
        } as unknown as AgentGUIComposerSettingsVM,
        capabilityControlsReadOnly: false,
        labels: {} as AgentComposerProps["labels"],
        uiLanguage: "en",
        editorHandleRef: { current: null }
      })
    );

    expect(
      result.current.slashPaletteEntries
        .filter((entry) => entry.type === "skill")
        .map((entry) => entry.label)
    ).toEqual(["github", "review"]);
  });

  it("removes connector entries immediately when the host disables them", () => {
    const { result } = renderHook(() =>
      useComposerPaletteCatalog({
        provider: "codex",
        paletteDraftPrompt: "/",
        availableCommands: [],
        availableSkills: [
          {
            name: "review",
            trigger: "$review",
            sourceKind: "project",
            kind: "skill"
          },
          {
            name: "GitHub",
            connectorKey: "github",
            trigger: "/github",
            sourceKind: "connector",
            kind: "connector",
            status: "available"
          }
        ],
        hasCompactableContext: false,
        compactSupported: false,
        composerSettings: {
          supportsPlanMode: false,
          supportsBrowser: false,
          supportsComputerUse: false,
          slashCommandPolicy: {
            fallbackCommands: [],
            commandEffects: [],
            commandCatalogAuthoritative: true
          }
        } as unknown as AgentGUIComposerSettingsVM,
        capabilityMenuState: { connectors: { enabled: false } },
        capabilityControlsReadOnly: false,
        labels: {} as AgentComposerProps["labels"],
        uiLanguage: "en",
        editorHandleRef: { current: null }
      })
    );

    expect(
      result.current.slashPaletteEntries
        .filter((entry) => entry.type === "skill")
        .map((entry) => entry.label)
    ).toEqual(["review"]);
  });
});
