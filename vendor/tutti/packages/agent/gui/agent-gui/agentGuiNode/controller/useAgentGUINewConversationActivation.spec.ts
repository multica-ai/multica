import { describe, expect, it } from "vitest";
import {
  resolveInitialRailPlacement,
  resolveNewConversationSettingProvenance,
  resolveSparseNewConversationActivationSettings
} from "./useAgentGUINewConversationActivation";

describe("resolveNewConversationSettingProvenance", () => {
  it("marks presented draft values inherited and required values explicit", () => {
    expect(resolveNewConversationSettingProvenance()).toEqual({
      modelExplicit: false,
      reasoningEffortExplicit: false
    });
    expect(
      resolveNewConversationSettingProvenance({
        model: "gpt-selected",
        reasoningEffort: "high"
      })
    ).toEqual({ modelExplicit: true, reasoningEffortExplicit: true });
  });
});

describe("resolveInitialRailPlacement", () => {
  it("uses the selected caller project section", () => {
    expect(
      resolveInitialRailPlacement({
        selectedProjectPath: "/workspace",
        userProjects: [
          {
            id: "project-1",
            label: "Workspace",
            path: "/workspace",
            pinnedAtUnixMs: 0,
            sectionKey: "project:/workspace"
          }
        ]
      })
    ).toEqual({
      version: 1,
      kind: "project",
      projectPath: "/workspace",
      sectionKey: "project:/workspace"
    });
  });

  it("uses conversations only when no project is selected", () => {
    expect(
      resolveInitialRailPlacement({
        selectedProjectPath: null,
        userProjects: []
      })
    ).toEqual({
      version: 1,
      kind: "conversations",
      sectionKey: "conversations"
    });
  });

  it("writes the canonical project root when the selected path is nested", () => {
    expect(
      resolveInitialRailPlacement({
        selectedProjectPath: "/workspace/packages/agent",
        userProjects: [
          {
            id: "project-1",
            label: "Workspace",
            path: "/workspace",
            pinnedAtUnixMs: 0,
            sectionKey: "project:/workspace"
          }
        ]
      })
    ).toEqual({
      version: 1,
      kind: "project",
      projectPath: "/workspace",
      sectionKey: "project:/workspace"
    });
  });

  it("fails closed when the selected project has no canonical section", () => {
    expect(
      resolveInitialRailPlacement({
        selectedProjectPath: "/workspace",
        userProjects: []
      })
    ).toBeNull();
  });
});

describe("resolveSparseNewConversationActivationSettings", () => {
  it("keeps presented full-access after the optimistic draft was retired", () => {
    expect(
      resolveSparseNewConversationActivationSettings({
        draftSettings: { model: "gpt-5.4" },
        composerOptions: {
          effectiveSettings: {
            model: "gpt-5.4",
            permissionModeId: "full-access"
          },
          permissionConfig: {
            configurable: true,
            defaultValue: "full-access",
            modes: [
              { id: "auto", label: "Approve for me", semantic: "auto" },
              {
                id: "full-access",
                label: "Full access",
                semantic: "full-access"
              }
            ]
          },
          models: [],
          reasoningEfforts: [],
          speeds: [],
          capabilities: {},
          behavior: {},
          skills: [],
          loadedAtUnixMs: 1
        } as never
      }).permissionModeId
    ).toBe("full-access");
  });

  it("prefers an explicit draft permission over remembered presentation", () => {
    expect(
      resolveSparseNewConversationActivationSettings({
        draftSettings: { permissionModeId: "auto" },
        composerOptions: {
          effectiveSettings: { permissionModeId: "full-access" },
          permissionConfig: {
            configurable: true,
            defaultValue: "full-access",
            modes: [
              { id: "auto", label: "Approve for me", semantic: "auto" },
              {
                id: "full-access",
                label: "Full access",
                semantic: "full-access"
              }
            ]
          },
          models: [],
          reasoningEfforts: [],
          speeds: [],
          capabilities: {},
          behavior: {},
          skills: [],
          loadedAtUnixMs: 1
        } as never
      }).permissionModeId
    ).toBe("auto");
  });

  it("clamps inherited OpenCode reasoning to the selected model catalog", () => {
    expect(
      resolveSparseNewConversationActivationSettings({
        draftSettings: {
          model: "openai/gpt-5.3-codex-spark",
          reasoningEffort: "none"
        },
        composerOptions: {
          provider: "opencode",
          effectiveSettings: {},
          reasoningOptionsByModel: {
            "openai/gpt-5.3-codex-spark": {
              defaultValue: "medium",
              options: [
                { label: "Low", value: "low" },
                { label: "Medium", value: "medium" }
              ]
            }
          },
          models: [],
          reasoningEfforts: [],
          speeds: [],
          capabilities: {},
          behavior: {},
          skills: [],
          loadedAtUnixMs: 1
        } as never
      }).reasoningEffort
    ).toBe("medium");
  });

  it("drops inherited OpenCode reasoning when model metadata is unavailable", () => {
    expect(
      resolveSparseNewConversationActivationSettings({
        draftSettings: {
          model: "openai/gpt-5.3-codex-spark",
          reasoningEffort: "none"
        },
        composerOptions: {
          provider: "opencode",
          effectiveSettings: {},
          models: [],
          reasoningEfforts: [],
          speeds: [],
          capabilities: {},
          behavior: {},
          skills: [],
          loadedAtUnixMs: 1
        } as never
      }).reasoningEffort
    ).toBeUndefined();
  });

  it("keeps an explicit required reasoning patch strict for Create", () => {
    expect(
      resolveSparseNewConversationActivationSettings({
        draftSettings: {
          model: "openai/gpt-5.3-codex-spark",
          reasoningEffort: "medium"
        },
        requiredSettingsPatch: { reasoningEffort: "none" },
        composerOptions: {
          provider: "opencode",
          effectiveSettings: {},
          reasoningOptionsByModel: {
            "openai/gpt-5.3-codex-spark": {
              defaultValue: "medium",
              options: [{ label: "Medium", value: "medium" }]
            }
          },
          models: [],
          reasoningEfforts: [],
          speeds: [],
          capabilities: {},
          behavior: {},
          skills: [],
          loadedAtUnixMs: 1
        } as never
      }).reasoningEffort
    ).toBe("none");
  });
});
