import { describe, expect, it } from "vitest";
import {
  appendAgentGUIComposerConnector,
  appendAgentGUIComposerFiles,
  appendAgentGUIComposerPrompt,
  resolveAgentGUIComposerAppendRequest,
  type AgentGUIComposerAppendRequest
} from "./useAgentGUIComposerAppendRequest";
import {
  agentComposerDraftFiles,
  agentComposerDraftConnectors,
  agentComposerDraftPrompt,
  agentComposerDraftQuotes,
  agentComposerDraftToPromptContent,
  buildAgentComposerDraft
} from "../model/agentComposerDraft";

const existingFile = {
  id: "existing",
  name: "existing.txt",
  path: "/tmp/existing.txt"
};
const archivedContextFile = {
  id: "archived-context-1",
  mimeType: "text/plain",
  name: "context.txt",
  path: "/tmp/context.txt",
  sizeBytes: 42
};

describe("appendAgentGUIComposerConnector", () => {
  it("selects a connector semantically without changing the typed prompt", () => {
    const draft = buildAgentComposerDraft({ prompt: "Summarize my workspace" });
    const next = appendAgentGUIComposerConnector(draft, "notion");

    expect(agentComposerDraftPrompt(next)).toBe("Summarize my workspace");
    expect(agentComposerDraftConnectors(next)).toEqual([
      { connectorKey: "notion" }
    ]);
    expect(appendAgentGUIComposerConnector(next, "notion")).toBe(next);
    expect(
      agentComposerDraftToPromptContent({ draft: next, skills: [] })
    ).toEqual([
      { type: "text", text: "Summarize my workspace" },
      { type: "connector", connectorKey: "notion" }
    ]);
  });
});

describe("appendAgentGUIComposerFiles", () => {
  it("preserves the public files contract and appends landed attachments", () => {
    const draft = buildAgentComposerDraft({
      files: [existingFile],
      prompt: "Fix this element"
    });
    const next = appendAgentGUIComposerFiles(draft, [archivedContextFile]);

    expect(agentComposerDraftPrompt(next)).toBe("Fix this element");
    expect(agentComposerDraftFiles(next)).toEqual([
      existingFile,
      archivedContextFile
    ]);
  });

  it("deduplicates appended attachments by id", () => {
    const draft = buildAgentComposerDraft({
      files: [archivedContextFile],
      prompt: ""
    });

    expect(
      agentComposerDraftFiles(
        appendAgentGUIComposerFiles(draft, [archivedContextFile])
      )
    ).toEqual([archivedContextFile]);
  });
});

describe("appendAgentGUIComposerPrompt", () => {
  it("appends a host-provided inline mention without creating an attachment", () => {
    const draft = buildAgentComposerDraft({ prompt: "Fix this" });
    const mention =
      "[@a](mention://browser-element/browser-element%3A1?path=%2Ftmp%2Felement.txt&tag=a&workspaceId=workspace-1)";
    const next = appendAgentGUIComposerPrompt(draft, mention);

    expect(agentComposerDraftPrompt(next)).toBe(`Fix this ${mention} `);
    expect(agentComposerDraftFiles(next)).toHaveLength(0);
    expect(appendAgentGUIComposerPrompt(next, mention)).toBe(next);
  });

  it("keeps multiple browser element mentions in one text-only message", () => {
    const first =
      "[@a](mention://browser-element/browser-element%3A1?path=%2Ftmp%2Fa.txt&tag=a&workspaceId=workspace-1)";
    const second =
      "[@div](mention://browser-element/browser-element%3A2?path=%2Ftmp%2Fdiv.txt&tag=div&workspaceId=workspace-1)";
    const withFirst = appendAgentGUIComposerPrompt(
      buildAgentComposerDraft({ prompt: "Review these" }),
      first
    );
    const withBoth = appendAgentGUIComposerPrompt(withFirst, second);

    expect(agentComposerDraftPrompt(withBoth)).toBe(
      `Review these ${first} ${second} `
    );
    expect(
      agentComposerDraftToPromptContent({ draft: withBoth, skills: [] })
    ).toEqual([
      {
        type: "text",
        text: `Review these ${first} ${second}`
      }
    ]);
    expect(agentComposerDraftFiles(withBoth)).toHaveLength(0);
  });
});

describe("resolveAgentGUIComposerAppendRequest", () => {
  it("replaces the current quote without changing text or attachments, only in its target session", () => {
    const draft = buildAgentComposerDraft({
      prompt: "Keep this question",
      files: [existingFile],
      quotes: [{ type: "quote", id: "old", text: "Old selection" }]
    });
    const request: AgentGUIComposerAppendRequest = {
      agentSessionId: "session-1", sequence: -1,
      quote: { type: "quote", id: "new", text: "New selection", sourceName: "Claude Code" }
    };
    expect(resolveAgentGUIComposerAppendRequest({
      activeConversationId: "session-2", draftByScopeKey: {}, handledSequence: null, request
    })).toBeNull();
    const resolved = resolveAgentGUIComposerAppendRequest({
      activeConversationId: "session-1", draftByScopeKey: { "session:session-1": draft }, handledSequence: null, request
    })!;
    expect(agentComposerDraftPrompt(resolved.nextDraft)).toBe("Keep this question");
    expect(agentComposerDraftFiles(resolved.nextDraft)).toEqual([existingFile]);
    expect(agentComposerDraftQuotes(resolved.nextDraft)).toEqual([{
      type: "quote", id: "new", text: "New selection", sourceName: "Claude Code"
    }]);
  });
  it("routes a connector selection to the exact active draft once", () => {
    const request: AgentGUIComposerAppendRequest = {
      agentSessionId: "session-1",
      connectorKey: "notion",
      sequence: 9
    };
    const resolved = resolveAgentGUIComposerAppendRequest({
      activeConversationId: "session-1",
      draftByScopeKey: {
        "session:session-1": buildAgentComposerDraft({ prompt: "Keep me" })
      },
      handledSequence: null,
      request
    });

    expect(agentComposerDraftPrompt(resolved!.nextDraft)).toBe("Keep me");
    expect(agentComposerDraftConnectors(resolved!.nextDraft)).toEqual([
      { connectorKey: "notion" }
    ]);
    expect(
      resolveAgentGUIComposerAppendRequest({
        activeConversationId: "session-1",
        draftByScopeKey: { "session:session-1": resolved!.nextDraft },
        handledSequence: 9,
        request
      })
    ).toBeNull();
  });

  it("waits for the exact target session before appending a routed prompt", () => {
    const request: AgentGUIComposerAppendRequest = {
      agentSessionId: "source-session",
      prompt: "Modify the managed issue",
      sequence: 8
    };

    expect(
      resolveAgentGUIComposerAppendRequest({
        activeConversationId: "other-session",
        draftByScopeKey: {
          "session:other-session": buildAgentComposerDraft({
            prompt: "Keep this draft"
          })
        },
        handledSequence: null,
        request
      })
    ).toBeNull();

    const resolved = resolveAgentGUIComposerAppendRequest({
      activeConversationId: "source-session",
      draftByScopeKey: {
        "session:source-session": buildAgentComposerDraft({
          prompt: "Existing source draft"
        })
      },
      handledSequence: null,
      request
    });
    expect(agentComposerDraftPrompt(resolved!.nextDraft)).toBe(
      "Existing source draft Modify the managed issue "
    );
  });

  it("keeps legacy external file requests as draft attachments", () => {
    const request: AgentGUIComposerAppendRequest = {
      files: [archivedContextFile],
      sequence: 6
    };

    const resolved = resolveAgentGUIComposerAppendRequest({
      activeConversationId: "session-1",
      draftByScopeKey: {
        "session:session-1": buildAgentComposerDraft({ prompt: "Keep me" })
      },
      handledSequence: null,
      request
    });

    expect(agentComposerDraftPrompt(resolved!.nextDraft)).toBe("Keep me");
    expect(agentComposerDraftFiles(resolved!.nextDraft)).toEqual([
      archivedContextFile
    ]);
  });

  it("resolves each request sequence once against the active draft scope", () => {
    const initialDraft = buildAgentComposerDraft({ prompt: "Keep me" });
    const browserElementMention =
      "[@a](mention://browser-element/browser-element%3A1?path=%2Ftmp%2Fbrowser-element.txt&tag=a&workspaceId=workspace-1)";
    const request: AgentGUIComposerAppendRequest = {
      prompt: browserElementMention,
      sequence: 7
    };

    const resolved = resolveAgentGUIComposerAppendRequest({
      activeConversationId: "session-1",
      draftByScopeKey: { "session:session-1": initialDraft },
      handledSequence: null,
      request
    });

    expect(resolved?.draftKey).toBe("session:session-1");
    expect(agentComposerDraftPrompt(resolved!.nextDraft)).toContain(
      "mention://browser-element/"
    );
    expect(agentComposerDraftFiles(resolved!.nextDraft)).toHaveLength(0);
    expect(
      agentComposerDraftToPromptContent({
        draft: resolved!.nextDraft,
        skills: []
      })
    ).toEqual([
      expect.objectContaining({
        type: "text",
        text: expect.stringContaining("mention://browser-element/")
      })
    ]);
    expect(
      resolveAgentGUIComposerAppendRequest({
        activeConversationId: "session-1",
        draftByScopeKey: { "session:session-1": resolved!.nextDraft },
        handledSequence: 7,
        request
      })
    ).toBeNull();
  });
});
