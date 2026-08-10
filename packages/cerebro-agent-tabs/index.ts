// FIR-4840 — the single registry of cerebro-owned tabs on the agent detail
// page.
//
// Before this package every cerebro tab package declared its own structurally
// identical `Agent*TabExtension` interface and each agent page re-listed the
// factories by hand. The redesigned page was written against that hand-written
// list and silently fell behind: `production_prompt` and `quality` shipped, but
// only the legacy page ever mounted them.
//
// Adding a tab is now one edit in one place — see `docs/agents/agent-detail-tabs.md`.

import type { ComponentType, ReactNode } from "react";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { createAgentCapabilitiesTabs } from "@multica/cerebro-agent-capabilities/views";
import { createAgentMemoryTabs } from "@multica/cerebro-agent-memory/views";
import { createAgentPromptSnapshotTabs } from "@multica/cerebro-agent-prompt/views";
import { createAgentQualityTabs } from "@multica/cerebro-agent-quality/views";
import { createAgentToolsTabs } from "@multica/cerebro-agent-tools/views";

/** Everything a tab is handed by the page that mounts it. */
export interface AgentDetailTabContext {
  agent: Agent;
  runtimes: AgentRuntime[];
  canEdit: boolean;
}

export interface AgentDetailTabExtension {
  /** Stable id — also the `?tab=` deep-link value. */
  id: string;
  /**
   * Key under `agents.tabs` in `packages/views/locales/*`. The legacy agent
   * page resolves it through i18n; the redesigned page renders `label`.
   */
  labelKey: string;
  icon: ComponentType<{ className?: string }>;
  render: (context: AgentDetailTabContext) => ReactNode;
}

/**
 * English tab labels for pages that render the strip without i18n. Keyed by
 * `labelKey`, so the values stay identical to `agents.tabs` in the locales.
 */
export const AGENT_DETAIL_TAB_LABELS: Record<string, string> = {
  tools: "Tools",
  capabilities: "Capabilities",
  memory: "Memory",
  production_prompt: "Production prompt",
  quality: "Quality",
};

/** Which registry entries are gated, and by which flag. */
export interface AgentDetailTabFlags {
  productionPromptOn: boolean;
  qualityOn: boolean;
}

/**
 * Pure form of the registry, so tab composition is testable without a feature
 * flag provider. Order here is the order the tabs appear on the page.
 */
export function agentDetailTabExtensions(
  flags: AgentDetailTabFlags,
): AgentDetailTabExtension[] {
  return [
    ...createAgentToolsTabs(),
    ...createAgentCapabilitiesTabs(),
    ...createAgentMemoryTabs(),
    ...(flags.productionPromptOn ? createAgentPromptSnapshotTabs() : []),
    ...(flags.qualityOn ? createAgentQualityTabs() : []),
  ];
}

/** What an agent detail page should call. */
export function useAgentDetailTabExtensions(): AgentDetailTabExtension[] {
  return agentDetailTabExtensions({
    productionPromptOn: useFeatureFlag("cerebro_agent_production_prompt"),
    qualityOn: useFeatureFlag("cerebro_agent_quality"),
  });
}
