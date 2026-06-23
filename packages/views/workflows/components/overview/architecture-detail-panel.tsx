"use client";

import type { WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { useT } from "../../../i18n";

export interface ArchitectureDetailPanelData {
  node: WorkflowNode;
  agent: Agent | null;
  plugin: BuiltinPlugin | null;
  criticAgent: Agent | null;
}

export interface ArchitectureDetailPanelProps {
  data: ArchitectureDetailPanelData;
  onClose: () => void;
  onOpenInEditor: () => void;
}

export function ArchitectureDetailPanel({
  data,
  onClose,
  onOpenInEditor,
}: ArchitectureDetailPanelProps) {
  const { t } = useT("workflows");
  const { node, agent, plugin, criticAgent } = data;

  const displayEntity = criticAgent ?? agent;
  const displayName = plugin?.name ?? displayEntity?.name ?? node.title;
  const displayDesc =
    plugin?.description ?? displayEntity?.description ?? node.description;

  return (
    <div
      className="fixed right-0 top-0 bottom-0 w-[380px] bg-background border-l shadow-lg z-50 overflow-y-auto"
      data-testid="architecture-detail-panel"
    >
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b sticky top-0 bg-background">
        <h2 className="font-semibold text-sm">
          {t(($) => $.overview.detail_panel.title)}
        </h2>
        <button
          onClick={onClose}
          className="text-muted-foreground hover:text-foreground text-lg leading-none"
          data-testid="detail-panel-close"
        >
          ×
        </button>
      </div>

      <div className="p-4 space-y-5">
        {/* ── Plugin / Entity info ── */}
        <section>
          <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-2">
            {criticAgent
              ? "Critic"
              : t(($) => $.overview.detail_panel.plugin_info)}
          </h3>
          <h4 className="font-medium text-sm">{displayName}</h4>
          {plugin?.slug && (
            <p className="text-xs text-muted-foreground mt-0.5">
              {plugin.slug}
            </p>
          )}
          {displayDesc && (
            <p className="text-xs text-muted-foreground mt-1">{displayDesc}</p>
          )}
          {plugin?.version && (
            <p className="text-xs text-muted-foreground mt-1">
              v{plugin.version}
            </p>
          )}
          {plugin?.category && (
            <p className="text-xs text-muted-foreground mt-0.5">
              {plugin.category}
            </p>
          )}
        </section>

        {/* ── Agent info ── */}
        {displayEntity && (
          <section>
            <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-2">
              {t(($) => $.overview.detail_panel.agent_info)}
            </h3>
            <AgentInfoBlock agent={displayEntity} />
          </section>
        )}
      </div>

      {/* Footer */}
      <div className="sticky bottom-0 bg-background border-t p-3">
        <button
          onClick={onOpenInEditor}
          className="w-full py-2 text-sm bg-primary text-primary-foreground rounded-md hover:opacity-90"
        >
          {t(($) => $.overview.detail_panel.open_in_editor)}
        </button>
      </div>
    </div>
  );
}

/** Renders the full agent info block. */
function AgentInfoBlock({ agent }: { agent: Agent }) {
  const fields: [string, string | number | boolean | null | undefined][] = [
    ["Name", agent.name],
    ["Description", agent.description],
    ["Runtime mode", agent.runtime_mode],
    ["Status", agent.status],
    ["Model", agent.model],
    ["Thinking level", agent.thinking_level || "—"],
    ["Visibility", agent.visibility],
    ["Max concurrent", agent.max_concurrent_tasks],
    ["Built-in", agent.is_builtin ? "Yes" : "No"],
    ["Instructions", agent.instructions],
    ["Custom env keys", Object.keys(agent.custom_env).join(", ") || "—"],
    ["Custom args", agent.custom_args.join(", ") || "—"],
    ["Skills", `${agent.skills.length}`],
  ];

  return (
    <dl className="space-y-1.5 text-xs">
      {fields.map(([label, value]) => {
        const displayValue =
          value === "" || value === null || value === undefined
            ? "—"
            : String(value);
        return (
          <div key={label} className="flex gap-2">
            <dt className="text-muted-foreground shrink-0 w-28">{label}</dt>
            <dd className="text-foreground truncate">{displayValue}</dd>
          </div>
        );
      })}
    </dl>
  );
}
