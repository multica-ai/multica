import { describe, expect, it } from "vitest";
import {
  mergeAgentActivityMessages,
  type AgentActivityMessage
} from "@tutti-os/agent-activity-core";
import {
  createOptimisticPromptMessage,
  normalizePromptContentBlocks
} from "./agentGuiController.promptHelpers";

// Step 9 (ADR 0004 desktop-half): optimistic echoes must not carry
// daemon-domain version numbers. Durable rows use the store's small monotonic
// counter; an echo minted from a ms wall-clock timestamp (~10^12) would win
// every version comparison against its durable twin and poison version-based
// cursors.
describe("createOptimisticPromptMessage", () => {
  const echoInput = {
    workspaceId: "room-1",
    agentSessionId: "session-1",
    turnId: "pending:submit-1",
    clientSubmitId: "submit-1",
    userId: "user-1",
    content: [{ type: "text" as const, text: "Ask" }],
    occurredAtUnixMs: 1_750_000_000_000
  };

  it("mints the echo outside the durable version domain", () => {
    const echo = createOptimisticPromptMessage(echoInput);
    expect(echo.version).toBe(0);
  });

  it("keeps presentation text separate from runtime content", () => {
    const echo = createOptimisticPromptMessage({
      ...echoInput,
      displayPrompt: "/browser",
      content: [{ type: "text", text: "runtime instructions" }]
    });
    expect(echo.payload).toMatchObject({
      content: [{ type: "text", text: "runtime instructions" }],
      displayPrompt: "/browser",
      text: "/browser"
    });
  });

  it("keeps image-only content without synthesizing presentation text", () => {
    const echo = createOptimisticPromptMessage({
      ...echoInput,
      content: [
        {
          type: "image",
          mimeType: "image/png",
          path: "/prompt-assets/screen.png",
          name: "screen.png"
        }
      ]
    });

    expect(echo.payload).toMatchObject({
      content: [
        {
          type: "image",
          mimeType: "image/png",
          path: "/prompt-assets/screen.png",
          name: "screen.png"
        }
      ],
      text: ""
    });
  });

  it("lets the durable twin replace the echo in an id-keyed merge", () => {
    const echo = createOptimisticPromptMessage(echoInput);
    const durableTwin: AgentActivityMessage = {
      ...echo,
      version: 3,
      turnId: "turn-2",
      payload: {
        actorId: "user-1",
        clientSubmitId: "submit-1",
        text: "Ask"
      }
    };
    const merged = mergeAgentActivityMessages([echo], [durableTwin]);
    expect(merged).toHaveLength(1);
    expect(merged[0]).toMatchObject({ version: 3, turnId: "turn-2" });
  });

});

describe("normalizePromptContentBlocks", () => {
  it("preserves a valid structured local connector selection", () => {
    expect(
      normalizePromptContentBlocks([
        { type: "text", text: "list my calendar events" },
        { type: "connector", connectorKey: " lark-cli " }
      ])
    ).toEqual([
      { type: "text", text: "list my calendar events" },
      { type: "connector", connectorKey: "lark-cli" }
    ]);
  });
});
