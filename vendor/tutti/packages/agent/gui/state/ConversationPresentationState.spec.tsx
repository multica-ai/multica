import { TranscriptModel } from "@multica/conversation-client/models";
import { SessionLoadService } from "@multica/conversation-client/services";
import { act, render, screen } from "@testing-library/react";
import type { AgentActivityMessage } from "@tutti-os/agent-activity-core";
import { autorun, observable, runInAction } from "mobx";
import { observer } from "mobx-react-lite";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AgentSessionChrome } from "../agent-gui/agentGuiNode/AgentSessionChrome";
import type { ConversationServices } from "../conversationServices";
import { createTestPromptOperations } from "../shared/testing/createTestPromptOperations";
import { createTestRuntimeState } from "../shared/testing/createTestRuntimeState";
import type {
  AgentGUITargetConnectionSource,
  AgentGUITargetConnectionState
} from "../types";
import { ActiveConversationState } from "./ActiveConversationState";
import { ComposerCapabilitiesState } from "./ComposerCapabilitiesState";
import { SessionPresentationHostService } from "./SessionPresentationHostService";
import type { SessionPresentationStateInput } from "./SessionPresentationState";
import { SessionPresentationState } from "./SessionPresentationState";

function fixture(source?: AgentGUITargetConnectionSource) {
  const runtime = createTestRuntimeState("w");
  const transcript = new TranscriptModel<AgentActivityMessage>();
  const update = observable.box<null>(null);
  const composer = {
    options: () => null,
    status: () => undefined,
    settingsUpdate: () => update.get(),
    updateSettings: vi.fn()
  };
  const services = {
    ...runtime,
    transcript,
    composer
  } as unknown as ConversationServices;
  const target = {
    agentTargetId: "local:codex",
    targetId: "local:codex",
    provider: "codex" as const,
    label: "Codex",
    ref: { kind: "local" as const, provider: "codex" as const }
  };
  const data = {
    provider: "codex" as const,
    agentTargetId: target.agentTargetId ?? null,
    lastActiveAgentSessionId: "s"
  };
  const selected = {
    data,
    provider: "codex" as const,
    targetId: target.targetId,
    agentTargetId: target.agentTargetId ?? null
  };
  const native = new ActiveConversationState({
    activeConversationId: "s",
    composer,
    prompts: runtime.prompts,
    conversationServices: runtime
  });
  const capabilities = new ComposerCapabilitiesState({
    get activeConversationId() {
      return native.activeConversationId;
    },
    get activeCanonicalSession() {
      return native.activeCanonicalSession;
    },
    get activeSessionState() {
      return native.activeSessionState;
    },
    data,
    draftSettingsBySessionId: {},
    selectedComposerTargetData: selected,
    composer
  });
  const presentationInput: SessionPresentationStateInput = {
    native,
    capabilities,
    conversation: () => null,
    selectedTarget: target,
    targets: [target],
    detailError: null,
    agentActivityRuntime: services,
    agentTargetsLoading: false,
    currentUserId: "u",
    isLoadingMessages: false,
    providerReadinessGate: null,
    selectedAgentTargetUnavailable: false,
    selectedAgentTargetUnavailableReason: null,
    selectedAgentTargetOwnerLabel: null,
    targetConnectionSource: source,
    workspaceId: "w",
    workspacePath: "/workspace",
    avoidGroupingEdits: false
  };
  const session = new SessionPresentationState(
    presentationInput,
    new SessionPresentationHostService(presentationInput)
  );

  function upsert(
    id = "s",
    pendingInteractions: unknown[] = [],
    activeTurn: unknown = null
  ) {
    runtime.sessions.upsertSession({
      agentSessionId: id,
      agentTargetId: target.agentTargetId,
      workspaceId: "w",
      provider: "codex",
      cwd: "/workspace",
      title: "Conversation",
      resumable: true,
      pendingInteractions,
      latestTurnInteractions: [],
      activeTurnId: (activeTurn as { turnId: string } | null)?.turnId ?? null,
      activeTurn
    } as never);
  }
  upsert();
  runtime.activations.request({
    mode: "existing",
    agentSessionId: "s",
    requestId: "open-s"
  });
  return { runtime, services, native, capabilities, session, upsert, composer };
}
class Connection implements AgentGUITargetConnectionSource {
  readonly listeners = new Set<() => void>();
  state: AgentGUITargetConnectionState = {
    status: "connecting",
    retryAttempt: 0
  };
  getConnectionState() {
    return this.state;
  }
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  set(state: AgentGUITargetConnectionState) {
    this.state = state;
    for (const listener of this.listeners) listener();
  }
}
afterEach(() => vi.useRealTimers());

describe("native conversation presentation", () => {
  it("updates the consuming controls for an exact question without rerendering their parent", () => {
    const f = fixture();
    let parent = 0,
      controls = 0;
    const Controls = observer(() => {
      controls++;
      return (
        <output>
          {f.session.pendingInteractivePrompt?.requestId ?? "none"}:
          {f.session.composerGate.submission.reason ?? "ready"}
        </output>
      );
    });
    function Parent() {
      parent++;
      return <Controls />;
    }
    const mounted = render(<Parent />);
    const before = controls;
    const interaction = {
      agentSessionId: "s",
      turnId: "t",
      requestId: "question",
      kind: "question",
      toolName: "AskUserQuestion",
      status: "pending",
      createdAtUnixMs: 1,
      updatedAtUnixMs: 1,
      input: {
        questions: [
          {
            id: "scope",
            header: "Scope",
            question: "Which scope?",
            allowFreeText: true,
            options: []
          }
        ]
      }
    };
    const turn = {
      agentSessionId: "s",
      turnId: "t",
      origin: "user_prompt",
      phase: "waiting",
      startedAtUnixMs: 1,
      updatedAtUnixMs: 1
    };
    act(() => f.upsert("s", [interaction], turn));
    expect(screen.getByText("question:conversation_busy")).toBeTruthy();
    expect(f.session.composerGate.submission.status).toBe("queue");
    expect(f.session.canAnswerPendingInteractivePromptFromComposer).toBe(true);
    expect(f.native.pendingQuestionComposerTarget).toEqual({
      agentSessionId: "s",
      turnId: "t",
      requestId: "question",
      questionIds: ["scope"]
    });
    expect(parent).toBe(1);
    expect(controls).toBe(before + 1);
    act(() => f.upsert("other"));
    expect(controls).toBe(before + 1);
    act(() =>
      f.upsert(
        "s",
        [interaction, { ...interaction, requestId: "second" }],
        turn
      )
    );
    expect(f.native.pendingQuestionComposerTarget).toBeNull();
    mounted.unmount();
    f.runtime.dispose();
  });
  it("releases delayed host subscriptions and keeps the ready gate stable across unrelated updates", () => {
    vi.useFakeTimers();
    const connection = new Connection(),
      f = fixture(connection);
    let renders = 0;
    const Controls = observer(() => {
      renders++;
      return <output>{f.session.composerGate.runtime.status}</output>;
    });
    const mounted = render(<Controls />);
    expect(screen.getByText("blocked")).toBeTruthy();
    expect(connection.listeners.size).toBeGreaterThan(0);
    act(() => connection.set({ status: "connected", retryAttempt: 0 }));
    expect(screen.getByText("ready")).toBeTruthy();
    const gate = f.session.composerGate,
      before = renders;
    act(() => f.upsert("other"));
    expect(f.session.composerGate).toBe(gate);
    expect(renders).toBe(before);
    mounted.unmount();
    expect(connection.listeners.size).toBe(0);
    expect(vi.getTimerCount()).toBe(0);
    f.runtime.dispose();
  });
  it("lets the status chrome observe native activation failures without a parent snapshot", () => {
    const f = fixture();
    let parent = 0;
    const source = {
      get chrome() {
        return f.session.sessionChrome;
      },
      get isRespondingApproval() {
        return f.session.isRespondingApproval;
      },
      get approvalDisabledReason() {
        return f.session.approvalDisabledReason;
      }
    };
    function Parent() {
      parent++;
      return (
        <AgentSessionChrome
          source={source}
          labels={{
            approvalRequired: "Approval",
            authRequired: "Sign in",
            activatingSession: "Connecting",
            retryActivation: "Retry",
            continueInNewConversation: "Continue"
          }}
          onSubmitApprovalOption={() => false}
          onRetryActivation={() => {}}
          onContinueInNewConversation={() => {}}
        />
      );
    }
    const mounted = render(<Parent />);
    act(() =>
      f.runtime.activations.recordFailure(
        { mode: "existing", agentSessionId: "s", requestId: "failed" },
        { code: null, message: "Could not resume" }
      )
    );
    expect(screen.getByText("Could not resume")).toBeTruthy();
    expect(parent).toBe(1);
    mounted.unmount();
    f.runtime.dispose();
  });
  it("keeps an accepted cancellation visible until the native operation settles", () => {
    const f = fixture(),
      status = observable.box<string | null>("requested");
    const native = new ActiveConversationState({
      activeConversationId: "s",
      composer: f.composer,
      conversationServices: f.runtime,
      prompts: createTestPromptOperations({ cancelStatus: () => status.get() })
    });
    const seen: boolean[] = [];
    const stop = autorun(() => {
      seen.push(native.isInterrupting);
    });
    runInAction(() => status.set("accepted"));
    expect(native.isInterrupting).toBe(true);
    runInAction(() => status.set(null));
    expect(seen).toEqual([true, false]);
    stop();
    f.runtime.dispose();
  });
  it("reads failed detail reconciliation and clears it after hydration", async () => {
    const f = fixture();
    const loads = new SessionLoadService({
      canLoad: () => true,
      load: async () => {
        throw new Error("detail reconcile failed");
      },
      describeError: (error) => ({
        code: null,
        message: (error as Error).message
      })
    });
    const native = new ActiveConversationState({
      activeConversationId: "s",
      composer: f.composer,
      prompts: f.runtime.prompts,
      conversationServices: { ...f.runtime, sessionLoads: loads }
    });
    loads.request("s", { state: true, messages: true });
    await vi.waitFor(() =>
      expect(native.activeSessionReconcileError).toBe("detail reconcile failed")
    );
    loads.markHydrated("s");
    expect(native.activeSessionReconcileError).toBeNull();
    loads.dispose();
    f.runtime.dispose();
  });
  it("reads provider commands without waiting for a session event", () => {
    const f = fixture();
    const options = {
      commands: [{ name: "memory", description: "Manage memory" }],
      skills: []
    };
    const capability = new ComposerCapabilitiesState({
      activeConversationId: null,
      activeCanonicalSession: null,
      activeSessionState: null,
      data: { provider: "codex", lastActiveAgentSessionId: null },
      draftSettingsBySessionId: {},
      selectedComposerTargetData: {
        data: { provider: "codex", lastActiveAgentSessionId: null },
        provider: "codex",
        targetId: "local:codex",
        agentTargetId: "local:codex"
      },
      composer: { options: () => options as never, status: () => undefined }
    });
    expect(capability.providerComposerOptions?.commands).toEqual(
      options.commands
    );
    f.runtime.dispose();
  });
});
