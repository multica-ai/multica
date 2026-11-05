import type { I18nRuntime } from "@tutti-os/ui-i18n-runtime";
import { TooltipProvider } from "@tutti-os/ui-system";
import { memo, useMemo, type JSX } from "react";
import {
  AgentGUINode,
  type AgentGUINodeProps
} from "./agent-gui/agentGuiNode/AgentGUINode";
import { AgentGUIActivityHostProvider } from "./agentActivityHost";
import {
  normalizeAgentGUIAgents,
  projectAgentGUIAgentsToTargets
} from "./agents";
import type { ConversationServices } from "./conversationServices";
import type { AgentHostInputApi } from "./host/agentHostApi";
import { AgentGuiI18nProvider, type AgentGuiI18nLocale } from "./i18n/index";
import { AgentVisibleErrorPresentationProvider } from "./shared/visibleError/AgentVisibleErrorPresentationContext";
import type {
  AgentGUIAgentDirectorySnapshot,
  AgentGUIHomeSuggestionId
} from "./types";

export type { ReferenceProvenanceCatalog as AgentGUIReferenceProvenanceFilterCatalog } from "@tutti-os/workspace-file-reference/contracts";
export type { AgentGUIHomeSuggestionId } from "./types";

type AgentGUIPublicHostCapabilities = Omit<
  AgentGUINodeProps["hostCapabilities"],
  | "agentTargets"
  | "agentTargetsLoading"
  | "mentionAgentTargets"
  | "handoffAgentTargets"
  | "handoffAgentTargetsLoading"
  | "providerRailMode"
  | "disabledHomeSuggestions"
>;

type AgentGUIPublicRenderSlots = AgentGUINodeProps["renderSlots"];

export interface AgentGUIProps
  extends Omit<AgentGUINodeProps, "hostCapabilities" | "renderSlots"> {
  agentDirectory: AgentGUIAgentDirectorySnapshot;
  /**
   * Complete host-owned identity catalog for rendered Agent mentions. Unlike
   * handoff, this directory may include unavailable targets because it does
   * not grant launch capability.
   */
  mentionAgentDirectory?: AgentGUIAgentDirectorySnapshot;
  /**
   * Host-owned launch catalog for conversation handoff. When omitted, handoff
   * uses `agentDirectory`, preserving the single-runtime host contract.
   */
  handoffAgentDirectory?: AgentGUIAgentDirectorySnapshot;
  agentActivityRuntime: ConversationServices;
  agentHostApi?: AgentHostInputApi | null;
  /** Starter entries to hide below the empty new-session composer. */
  disabled?: readonly AgentGUIHomeSuggestionId[];
  i18n?: I18nRuntime<string> | null;
  locale?: AgentGuiI18nLocale;
  hostCapabilities: AgentGUIPublicHostCapabilities;
  renderSlots: AgentGUIPublicRenderSlots;
}

export const AgentGUI = memo(function AgentGUI({
  agentActivityRuntime,
  agentHostApi,
  agentDirectory,
  mentionAgentDirectory,
  handoffAgentDirectory,

  disabled,
  i18n,
  locale,
  ...props
}: AgentGUIProps): JSX.Element {
  const normalizedAgents = useMemo(
    () => normalizeAgentGUIAgents(agentDirectory.agents),
    [agentDirectory.agents]
  );
  const agentTargets = useMemo(
    () => projectAgentGUIAgentsToTargets(normalizedAgents),
    [normalizedAgents]
  );
  const hostCapabilities = props.hostCapabilities;
  const effectiveHandoffAgentDirectory =
    handoffAgentDirectory ?? agentDirectory;
  const normalizedHandoffAgents = useMemo(
    () =>
      effectiveHandoffAgentDirectory.agents === agentDirectory.agents
        ? normalizedAgents
        : normalizeAgentGUIAgents(effectiveHandoffAgentDirectory.agents),
    [
      agentDirectory.agents,
      effectiveHandoffAgentDirectory.agents,
      normalizedAgents
    ]
  );
  const handoffAgentTargets = useMemo(() => {
    const targets =
      normalizedHandoffAgents === normalizedAgents
        ? agentTargets
        : projectAgentGUIAgentsToTargets(normalizedHandoffAgents);
    if (
      !targets.some(
        (target) =>
          target.disabled !== true &&
          (target.availability?.status !== "ready" ||
            hostCapabilities?.providerReadinessGates?.[target.provider] != null)
      )
    ) {
      return targets;
    }
    return targets.map((target) =>
      (target.availability?.status === "ready" &&
        hostCapabilities?.providerReadinessGates?.[target.provider] == null) ||
      target.disabled === true
        ? target
        : { ...target, disabled: true }
    );
  }, [
    agentTargets,
    hostCapabilities?.providerReadinessGates,
    normalizedAgents,
    normalizedHandoffAgents
  ]);
  const renderSlots = props.renderSlots;
  const nodeHostCapabilities = useMemo<AgentGUINodeProps["hostCapabilities"]>(
    () => ({
      ...hostCapabilities,
      agentTargets,
      agentTargetsLoading:
        agentDirectory.agents.length === 0 &&
        (agentDirectory.status === "idle" ||
          agentDirectory.status === "loading"),
      mentionAgentTargets: mentionAgentDirectory
        ? projectAgentGUIAgentsToTargets(
            normalizeAgentGUIAgents(mentionAgentDirectory.agents)
          )
        : undefined,
      handoffAgentTargets,
      handoffAgentTargetsLoading:
        effectiveHandoffAgentDirectory.agents.length === 0 &&
        (effectiveHandoffAgentDirectory.status === "idle" ||
          effectiveHandoffAgentDirectory.status === "loading"),
      disabledHomeSuggestions: disabled,

      providerRailMode: "exact"
    }),
    [
      agentDirectory.agents.length,
      agentDirectory.status,
      agentTargets,

      disabled,
      effectiveHandoffAgentDirectory.agents.length,
      effectiveHandoffAgentDirectory.status,
      handoffAgentTargets,
      hostCapabilities,
      mentionAgentDirectory?.agents
    ]
  );
  const nodeRenderSlots = useMemo<AgentGUINodeProps["renderSlots"]>(
    () => ({
      ...renderSlots
    }),
    [renderSlots]
  );
  const nodeProps: AgentGUINodeProps = {
    ...props,
    hostCapabilities: nodeHostCapabilities,
    renderSlots: nodeRenderSlots
  };
  const content = (
    <AgentGuiI18nProvider runtime={i18n} locale={locale}>
      <AgentGUIActivityHostProvider
        agentActivityRuntime={agentActivityRuntime}
        agentHostApi={agentHostApi}
      >
        <AgentVisibleErrorPresentationProvider
          scope={props.hostCapabilities?.visibleErrorPresentationScope}
          value={props.hostCapabilities?.visibleErrorPresentationOverrides}
        >
          <AgentGUINode {...nodeProps} />
        </AgentVisibleErrorPresentationProvider>
      </AgentGUIActivityHostProvider>
    </AgentGuiI18nProvider>
  );
  return (
    <TooltipProvider delayDuration={120} skipDelayDuration={0}>
      {content}
    </TooltipProvider>
  );
});
