import { describe, expect, it, vi } from "vitest";
import type { ConversationServices } from "../../../conversationServices";
import { trackAgentGUISettingsProjectChange } from "./agentGuiProjectAnalytics";

describe("trackAgentGUISettingsProjectChange", () => {
  it.each(["clear", "create_new", "select_existing"] as const)(
    "tracks draft %s project changes with a null session id",
    (action) => {
      const trackSettingsProjectChange = vi.fn(async () => undefined);

      trackAgentGUISettingsProjectChange({
        agentActivityRuntime: {
          diagnostics: { trackSettingsProjectChange }
        } as unknown as ConversationServices,
        agentSessionId: null,
        metadata: { action },
        provider: "codex",
        workspaceId: "room-1"
      });

      expect(trackSettingsProjectChange).toHaveBeenCalledWith({
        action,
        agentSessionId: null,
        provider: "codex",
        workspaceId: "room-1"
      });
    }
  );

  it("does not track state synchronization without interaction metadata", () => {
    const trackSettingsProjectChange = vi.fn(async () => undefined);

    trackAgentGUISettingsProjectChange({
      agentActivityRuntime: {
        diagnostics: { trackSettingsProjectChange }
      } as unknown as ConversationServices,
      agentSessionId: null,
      provider: "codex",
      workspaceId: "room-1"
    });

    expect(trackSettingsProjectChange).not.toHaveBeenCalled();
  });
});
