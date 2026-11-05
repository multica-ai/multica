import { act, renderHook } from "@testing-library/react";
import { normalizeAgentActivitySession } from "@tutti-os/agent-activity-core";
import { observable, runInAction } from "mobx";
import { describe, expect, it, vi } from "vitest";
import { createTestPromptOperations } from "../../../shared/testing/createTestPromptOperations";
import { createTestRuntimeState } from "../../../shared/testing/createTestRuntimeState";
import type { AgentGUISessionSettingsUpdate } from "./agentGuiController.providerHelpers";
import { useAgentGUISessionState } from "./useAgentGUISessionState";

function composerFixture() {
  const update = observable.box<AgentGUISessionSettingsUpdate | null>(null, {
    deep: false
  });
  return {
    composer: {
      settingsUpdate: () => update.get(),
      updateSettings: vi.fn()
    },
    show(
      status: AgentGUISessionSettingsUpdate["status"] | null,
      settings: AgentGUISessionSettingsUpdate["settings"] = null,
      queuedSettings: AgentGUISessionSettingsUpdate["queuedSettings"] = null
    ) {
      runInAction(() =>
        update.set(
          status && {
            errorCode: null,
            errorMessage: null,
            queuedSettings,
            settings,
            status
          }
        )
      );
    }
  };
}
const prompts = createTestPromptOperations();

describe("useAgentGUISessionState", () => {
  it("shows an optimistic session selection then silently restores canonical settings on failure", () => {
    const conversationServices = createTestRuntimeState("workspace-1");
    conversationServices.sessions.upsertSessions([
      normalizeAgentActivitySession({
        activeTurnId: null,
        agentSessionId: "session-1",
        agentTargetId: "local:opencode",
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "opencode",
        settings: { permissionModeId: "ask" },
        title: "Session 1",
        workspaceId: "workspace-1"
      })
    ]);
    const { composer, show } = composerFixture();
    const rendered = renderHook(() =>
      useAgentGUISessionState({
        activeConversationId: "session-1",
        composer,
        prompts,
        conversationServices
      })
    );

    act(() => show("inFlight", { permissionModeId: "full-access" }));
    expect(
      rendered.result.current.activeCanonicalComposerSettings.permissionModeId
    ).toBe("full-access");

    act(() => show("failed", { permissionModeId: "full-access" }));
    expect(
      rendered.result.current.activeCanonicalComposerSettings.permissionModeId
    ).toBe("ask");
    expect(rendered.result.current.activeSessionError).toBeNull();
  });

  it("keeps the latest queued settings visible while the session runtime reconnects", () => {
    const conversationServices = createTestRuntimeState("workspace-1");
    const session = (settings: { model: string; planMode: boolean }) =>
      normalizeAgentActivitySession({
        activeTurnId: null,
        agentSessionId: "session-1",
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        settings,
        title: "Session 1",
        workspaceId: "workspace-1"
      });
    conversationServices.sessions.upsertSessions([
      session({ model: "model-canonical", planMode: false })
    ]);
    const { composer, show } = composerFixture();
    const rendered = renderHook(() =>
      useAgentGUISessionState({
        activeConversationId: "session-1",
        composer,
        prompts,
        conversationServices
      })
    );

    act(() =>
      show(
        "waitingForRuntime",
        { model: "model-first" },
        { model: "model-latest", planMode: true }
      )
    );
    expect(
      rendered.result.current.activeCanonicalComposerSettings
    ).toMatchObject({ model: "model-latest", planMode: true });

    act(() => show("inFlight", { model: "model-latest", planMode: true }));
    expect(
      rendered.result.current.activeCanonicalComposerSettings
    ).toMatchObject({ model: "model-latest", planMode: true });

    act(() => {
      conversationServices.sessions.upsertSessions([
        session({ model: "model-latest", planMode: true })
      ]);
      show(null);
    });
    expect(
      rendered.result.current.activeCanonicalComposerSettings
    ).toMatchObject({ model: "model-latest", planMode: true });
  });

  it("observes runtime availability for the selected session only", () => {
    const conversationServices = createTestRuntimeState("workspace-1");
    conversationServices.sessions.upsertSessions(
      ["session-1", "session-2"].map((agentSessionId) =>
        normalizeAgentActivitySession({
          activeTurnId: null,
          agentSessionId,
          cwd: "/workspace",
          latestTurnInteractions: [],
          pendingInteractions: [],
          provider: "codex",
          title: "Session 1",
          workspaceId: "workspace-1"
        })
      )
    );
    const rendered = renderHook(
      ({ activeConversationId }) =>
        useAgentGUISessionState({
          activeConversationId,
          composer: composerFixture().composer,
          prompts,
          conversationServices
        }),
      { initialProps: { activeConversationId: "session-1" } }
    );

    act(() => {
      conversationServices.sessions.setRuntimeAvailability("session-1", {
        state: "blocked",
        reason: "transport_reconnecting"
      });
    });
    expect(rendered.result.current.activeEngineRuntimeAvailability).toEqual({
      state: "blocked",
      reason: "transport_reconnecting"
    });

    rendered.rerender({ activeConversationId: "session-2" });
    expect(rendered.result.current.activeEngineRuntimeAvailability).toEqual({
      state: "available"
    });
  });
});
