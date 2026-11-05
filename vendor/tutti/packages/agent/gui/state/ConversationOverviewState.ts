import type { AgentActivityDisplayStatus } from "@tutti-os/agent-activity-core";
import { autorun, computed, makeObservable } from "mobx";
import { mergeVisibleConversations } from "../agent-gui/agentGuiNode/controller/agentGuiController.conversationHelpers";
import { conversationStatusFromAgentActivityDisplayStatus } from "../agent-gui/agentGuiNode/controller/agentGuiController.draftMessageHelpers";
import { normalizeOptionalText } from "../agent-gui/agentGuiNode/controller/agentGuiController.promptHelpers";
import { composerTargetDataFromProviderTarget } from "../agent-gui/agentGuiNode/controller/agentGuiController.providerHelpers";
import {
  conversationSummariesRenderEqual,
  stableConversationSummaryList
} from "../agent-gui/agentGuiNode/controller/agentGuiController.stableHelpers";
import { resolveConversationSummaryById } from "../agent-gui/agentGuiNode/controller/useAgentConversationSelection";
import {
  applyAgentGUIConversationProjects,
  type AgentGUIConversationSummary
} from "../agent-gui/agentGuiNode/model/agentGuiConversationModel";
import { rememberAgentGUIActiveConversation } from "../agent-gui/agentGuiNode/model/agentGuiSessionNavigationMemory";
import {
  agentGUIAgentTargetRefsEqual,
  resolveAgentGUIAgentTarget
} from "../agentTargets";
import type { AgentHostUserProject } from "../host/agentHostApi";
import { isAgentGUIProviderUnresolved } from "../shared/agentConversationTitleProjection.ts";
import type { AgentGUIAgentTarget, AgentGUINodeData } from "../types";
interface CurrentValue<T> {
  current: T;
}
export interface ConversationOverviewInput {
  activeConversationId: string | null;
  activityDisplayStatuses: ReadonlyMap<string, AgentActivityDisplayStatus>;
  conversations: readonly AgentGUIConversationSummary[];
  currentUserId: string | null | undefined;
  data: AgentGUINodeData;
  dataRef: CurrentValue<AgentGUINodeData>;
  defaultAgentTargetId: string | null;
  normalizedExplicitProviderTargets: readonly AgentGUIAgentTarget[];
  normalizedProviderTargets: readonly AgentGUIAgentTarget[];
  onDataChangeRef: CurrentValue<
    (updater: (current: AgentGUINodeData) => AgentGUINodeData) => void
  >;
  agentTargetsLoading: boolean;
  shouldUseStaticProviderTargets: boolean;
  transientConversation: AgentGUIConversationSummary | null;
  userProjects: readonly AgentHostUserProject[];
  workspacePath: string;
}
export class ConversationOverviewState {
  constructor(private readonly input: ConversationOverviewInput) {
    makeObservable(this, {
      conversationProjection: computed.struct,
      visibleConversations: computed.struct,
      activeConversation: computed.struct
    });
  }
  private readonly visibleConversationsRef = {
    current: null as AgentGUIConversationSummary[] | null
  };
  get conversationProjection() {
    const source = mergeVisibleConversations(
      this.input.conversations,
      this.input.transientConversation
    );
    const mapped = source.map((conversation) => {
      const activityStatus = conversationStatusFromAgentActivityDisplayStatus(
        this.input.activityDisplayStatuses.get(conversation.id)
      );
      return activityStatus && conversation.status !== activityStatus
        ? { ...conversation, status: activityStatus }
        : conversation;
    });
    const next = applyAgentGUIConversationProjects(
      mapped,
      this.input.userProjects
    );
    // Semantic conversations keep hidden entries so an explicitly opened
    // invisible session resolves its real identity; the rail list never
    // renders them.
    const visibleConversations = stableConversationSummaryList(
      this.visibleConversationsRef.current,
      next.filter((conversation) => !conversation.hiddenFromRail)
    );
    this.visibleConversationsRef.current = visibleConversations;
    return { semanticConversations: next, visibleConversations };
  }
  get visibleConversations() {
    return this.conversationProjection.visibleConversations;
  }
  private readonly activeConversationRef = {
    current: null as AgentGUIConversationSummary[] | null
  };
  get activeConversation() {
    const stabilize = (
      next: AgentGUIConversationSummary | null
    ): AgentGUIConversationSummary | null => {
      const previous = this.activeConversationRef.current?.[0] ?? null;
      const canReusePrevious =
        previous !== null &&
        next !== null &&
        conversationSummariesRenderEqual(previous, next) &&
        previous.agentTargetId === next.agentTargetId &&
        previous.resumable === next.resumable;
      const stable = next ? [canReusePrevious ? previous : next] : [];
      this.activeConversationRef.current = stable;
      return stable[0] ?? null;
    };
    const resolved = resolveConversationSummaryById(
      this.conversationProjection.semanticConversations,
      this.input.activeConversationId
    );
    if (resolved) {
      const status =
        conversationStatusFromAgentActivityDisplayStatus(
          this.input.activityDisplayStatuses.get(resolved.id)
        ) ?? resolved.status;
      return stabilize(
        status === resolved.status ? resolved : { ...resolved, status }
      );
    }
    if (!this.input.activeConversationId) return stabilize(null);
    const fallbackAgentTarget = resolveAgentGUIAgentTarget({
      agentTargetId: this.input.data.agentTargetId,
      defaultAgentTargetId: this.input.defaultAgentTargetId,
      provider: this.input.data.provider,
      agentTargets: this.input.normalizedProviderTargets,
      useStaticCatalog: this.input.shouldUseStaticProviderTargets
    });
    const activityStatus = conversationStatusFromAgentActivityDisplayStatus(
      this.input.activityDisplayStatuses.get(this.input.activeConversationId)
    );
    const previousActiveConversation = this.activeConversationRef.current?.[0];
    const fallbackUpdatedAtUnixMs =
      previousActiveConversation?.id === this.input.activeConversationId
        ? previousActiveConversation.updatedAtUnixMs
        : Date.now();
    return stabilize({
      id: this.input.activeConversationId,
      agentTargetId:
        normalizeOptionalText(this.input.data.agentTargetId) ??
        fallbackAgentTarget?.agentTargetId,
      userId: this.input.currentUserId?.trim() || undefined,
      provider: this.input.data.provider,
      title: "",
      titleFallback: "untitled-conversation",
      status: activityStatus ?? "ready",
      cwd: this.input.workspacePath,
      project: null,
      sortTimeUnixMs: fallbackUpdatedAtUnixMs,
      updatedAtUnixMs: fallbackUpdatedAtUnixMs
    });
  }
  startRemembering(): () => void {
    return autorun(() => {
      if (this.input.agentTargetsLoading || !this.input.activeConversationId) {
        return;
      }
      const summary = resolveConversationSummaryById(
        this.input.conversations,
        this.input.activeConversationId,
        this.input.transientConversation
      );
      if (!summary || isAgentGUIProviderUnresolved(summary.provider)) return;
      const summaryAgentTargetId = normalizeOptionalText(summary.agentTargetId);
      const providerMismatch =
        this.input.dataRef.current.provider !== summary.provider;
      const agentTargetMismatch =
        summaryAgentTargetId !== null &&
        normalizeOptionalText(this.input.dataRef.current.agentTargetId) !==
          summaryAgentTargetId;
      const rememberedSessionMismatch =
        summaryAgentTargetId !== null &&
        this.input.dataRef.current.lastActiveAgentSessionIdByAgentTargetId?.[
          summaryAgentTargetId
        ] !== this.input.activeConversationId;
      if (
        !providerMismatch &&
        !agentTargetMismatch &&
        !rememberedSessionMismatch
      ) {
        return;
      }
      const sessionTarget = resolveAgentGUIAgentTarget({
        agentTargetId: summaryAgentTargetId,
        defaultAgentTargetId: this.input.defaultAgentTargetId,
        provider: summary.provider,
        agentTargets: this.input.normalizedProviderTargets,
        useStaticCatalog: this.input.shouldUseStaticProviderTargets
      });
      if (!sessionTarget || sessionTarget.provider !== summary.provider) return;
      if (
        !providerMismatch &&
        summaryAgentTargetId !== null &&
        (sessionTarget.agentTargetId?.trim() ?? "") !== summaryAgentTargetId
      ) {
        return;
      }
      const sessionTargetIsExplicit =
        this.input.normalizedExplicitProviderTargets.some(
          (target) =>
            target.provider === sessionTarget.provider &&
            target.targetId === sessionTarget.targetId &&
            agentGUIAgentTargetRefsEqual(target.ref, sessionTarget.ref)
        );
      this.input.onDataChangeRef.current((current) => {
        const targetData = composerTargetDataFromProviderTarget({
          current,
          isExplicit: sessionTargetIsExplicit,
          target: sessionTarget
        });
        const nextData = rememberAgentGUIActiveConversation(
          targetData.data,
          this.input.activeConversationId,
          summaryAgentTargetId
        );
        if (
          current.provider === targetData.provider &&
          normalizeOptionalText(current.agentTargetId) ===
            targetData.agentTargetId &&
          (summaryAgentTargetId === null ||
            current.lastActiveAgentSessionIdByAgentTargetId?.[
              summaryAgentTargetId
            ] === this.input.activeConversationId)
        ) {
          return current;
        }
        this.input.dataRef.current = nextData;
        return nextData;
      });
    });
  }
}
