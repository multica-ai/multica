// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createAgentSessionEngine } from "@tutti-os/agent-activity-core";
import { createRichTextMentionMarkdown } from "@tutti-os/ui-rich-text/core";
import { AgentGuiI18nProvider } from "../../../vendor/tutti/packages/agent/gui/i18n/index";
import { useAgentGUISubmitInteractionActions } from "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/controller/useAgentGUISubmitInteractionActions";
import { AgentRichTextEditor } from "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/agentRichText/AgentRichTextEditor";

type Input = Parameters<typeof useAgentGUISubmitInteractionActions>[0];
let root: Root;
let container: HTMLDivElement;
let actions: ReturnType<typeof useAgentGUISubmitInteractionActions>;
const engines: ReturnType<typeof createAgentSessionEngine>[] = [];
beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  container = document.createElement("div"); document.body.append(container); root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount()); container.remove();
  for (const engine of engines.splice(0)) engine.dispose();
  vi.unstubAllGlobals();
});

async function mount({ waiting = true, mentioned = false, active = true, editor = false, locale = "zh-CN" as "en" | "zh-CN" } = {}) {
  const sessionEngine = createAgentSessionEngine({
    identity: { workspaceId: "workspace", origin: "multica" },
    clock: { nowUnixMs: () => Date.now() }, scheduler: { schedule: () => ({ cancel() {} }) },
    commandPort: { kind: "typed", effects: {}, execute: async () => undefined } as never,
  });
  engines.push(sessionEngine);
  const submit = vi.spyOn(sessionEngine, "submitPrompt").mockReturnValue({ accepted: true, queued: true });
  const respond = vi.fn(() => true);
  const mention = mentioned ? createRichTextMentionMarkdown({ providerId: "agent-target", entityId: "bug-agent", label: "捉虫高手", scope: { workspaceId: "workspace" } }) : "";
  const content: [{ type: "text"; text: string }] = [{ type: "text", text: `${mention}帮我记个优化` }];
  const draft = { current: { "session:session": content } as Input["draftByScopeKeyRef"]["current"] };
  const setDraftByScopeKey = vi.fn<Input["setDraftByScopeKey"]>((update) => { draft.current = typeof update === "function" ? update(draft.current) : update; });
  const setDetailError = vi.fn();
  const input = {
    activation: { activate: vi.fn(), codeFor: () => null, errorFor: () => null },
    activeConversationId: "session", activeConversationIdRef: { current: "session" },
    activeEngineActiveTurn: active ? { turnId: "active" } : null,
    activeEnginePendingInteractions: waiting ? [{ agentSessionId: "session", turnId: "active", requestId: "question", kind: "question", status: "pending", createdAtUnixMs: 1, updatedAtUnixMs: 1,
      input: { questions: [{ id: "scope", header: "排查范围", question: "查哪里？", allowFreeText: true, options: [] }] } }] : [],
    agentActivityRuntime: { prompts: { respondToInteraction: respond, interactionResponse: () => null, subscribeInteractionResponses: () => () => undefined } }, conversationListQuery: {}, conversationsRef: { current: [] }, dataRef: { current: {} },
    draftByScopeKeyRef: draft, executePromptRef: { current: vi.fn() },
    isComposerHomeRef: { current: false }, isCurrentConversation: () => true, isRespondingToInteraction: false,
    isSessionMarkedNonResumable: () => false, persistActiveConversation: vi.fn(),
    planActionsRef: { current: { implement: vi.fn(), feedback: vi.fn(), skip: vi.fn() } },
    promptImagesSupported: true, sessionEngine, setActiveConversationId: vi.fn(), setDetailError, setDraftByScopeKey,
    setGoalClearNoticeSequence: vi.fn(), setIntent: vi.fn(), submittedDraftSnapshotsRef: { current: {} },
    startConversation: vi.fn(), submitPromptRef: { current: vi.fn() }, transientConversation: null, workspaceId: "workspace",
  } as unknown as Input;
  function Probe() {
    actions = useAgentGUISubmitInteractionActions(input);
    return editor ? <AgentRichTextEditor value={content[0]!.text} disabled={false} placeholder="Prompt" onChange={vi.fn()}
      onSubmit={() => actions.submitPrompt(content)} onSubmitGuidance={() => actions.submitGuidancePrompt(content)} /> : null;
  }
  await act(async () => root.render(<AgentGuiI18nProvider locale={locale}><Probe /></AgentGuiI18nProvider>));
  return { content, draft, submit, respond, setDetailError, setDraftByScopeKey, input };
}

it.each([true, false])("retains a guidance draft before submission with an active turn (waiting: %s)", async (waiting) => {
  const state = await mount({ waiting, mentioned: true });
  await act(async () => actions.submitGuidancePrompt(state.content));
  expect(state.submit).not.toHaveBeenCalled();
  expect(state.respond).not.toHaveBeenCalled();
  expect(state.setDraftByScopeKey).not.toHaveBeenCalled();
  expect(state.draft.current["session:session"]).toEqual(state.content);
  expect(state.setDetailError).toHaveBeenLastCalledWith(expect.stringContaining("普通发送"));
});

it("uses the current English locale for an unsupported guidance action", async () => {
  const state = await mount({ locale: "en" });
  await act(async () => actions.submitGuidancePrompt(state.content));
  expect(state.setDetailError).toHaveBeenLastCalledWith(expect.stringContaining("queue"));
  expect(state.submit).not.toHaveBeenCalled();
});

it.each([
  { active: true, waiting: true },
  { active: true, waiting: false },
  { active: false, waiting: false },
])("queues ordinary agent mentions without guidance: %j", async ({ active, waiting }) => {
  const state = await mount({ waiting, mentioned: true, active });
  await act(async () => actions.submitPrompt(state.content));
  expect(state.submit).toHaveBeenCalledOnce();
  const input = state.submit.mock.calls[0]![0];
  expect(input.content).toEqual(state.content);
  expect(input.targetTurnId).toBeUndefined();
  expect(input.routing).toBeUndefined();
  expect(input.capabilityRefs).toBeUndefined();
  expect(state.respond).not.toHaveBeenCalled();
});

it.each(["metaKey", "ctrlKey"])("preserves a waiting-question draft on %s+Enter and allows ordinary Enter afterward", async (modifier) => {
  const state = await mount({ mentioned: true, editor: true });
  const editor = await vi.waitFor(() => {
    const element = container.querySelector<HTMLElement>('[contenteditable="true"]');
    expect(element).not.toBeNull(); return element!;
  });
  await act(async () => editor.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", [modifier]: true, bubbles: true, cancelable: true })));
  expect(state.submit).not.toHaveBeenCalled();
  expect(state.respond).not.toHaveBeenCalled();
  expect(state.setDetailError).toHaveBeenLastCalledWith(expect.stringContaining("普通发送"));
  expect(state.draft.current["session:session"]).toEqual(state.content);
  expect(editor.textContent).toContain("帮我记个优化");
  await act(async () => editor.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true })));
  expect(state.respond).not.toHaveBeenCalled();
  expect(state.submit).toHaveBeenCalledOnce();
  expect(state.submit.mock.calls[0]![0].targetTurnId).toBeUndefined();
});

it("still answers the pending question through ordinary submission", async () => {
  const state = await mount();
  await act(async () => actions.submitPrompt(state.content));
  expect(state.respond).toHaveBeenCalledWith(expect.objectContaining({ requestId: "question", turnId: "active", payload: expect.objectContaining({ answersByQuestionId: { scope: "帮我记个优化" } }) }));
  expect(state.submit).not.toHaveBeenCalled();
  expect(state.draft.current["session:session"]).toEqual([{ type: "text", text: "" }]);
});
