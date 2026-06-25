"use client";

import { useEffect } from "react";
import type { WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../../i18n";
import { ArrowUpRight, Bot, ShieldAlert, X, Package, Puzzle, Wrench } from "lucide-react";

export interface ArchitectureDetailPanelData {
  node: WorkflowNode;
  agent: Agent | null;
  plugin: BuiltinPlugin | null;
  criticAgent: Agent | null;
  focus?: "worker" | "critic";
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

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onClose]);

  const isCritic = data.focus === "critic" || Boolean(criticAgent && !agent);
  const displayEntity = criticAgent ?? agent;

  return (
    <>
      <button
        type="button"
        aria-label="Close details"
        onClick={onClose}
        className="fixed inset-0 z-40 bg-slate-950/18 backdrop-blur-[1px]"
      />
      <div
        className="fixed right-0 top-0 bottom-0 z-50 w-[520px] overflow-y-auto border-l bg-background/98 shadow-[0_20px_60px_rgba(15,23,42,0.18)] backdrop-blur"
        data-testid="architecture-detail-panel"
      >
        {/* Header */}
        <div className="sticky top-0 z-10 flex items-center justify-between border-b bg-background/95 px-4 py-3 backdrop-blur">
          <div>
            <div className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
              {t(($) => $.overview.detail_panel.inspector)}
            </div>
            <h2 className="text-sm font-semibold">
              {t(($) => $.overview.detail_panel.title)}
            </h2>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex h-8 w-8 items-center justify-center rounded-full border border-border/70 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            data-testid="detail-panel-close"
            aria-label="Close"
          >
            <X className="h-4 w-4" strokeWidth={2} />
          </button>
        </div>

        <div className="space-y-4 p-4">
          {/* ── Node identity header ── */}
          <section className="rounded-2xl border bg-muted/35 p-4">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <span className="inline-flex items-center gap-1.5 rounded-full bg-background px-2.5 py-0.5 text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground border border-border/40">
                  {isCritic ? (
                    <ShieldAlert className="h-3 w-3" strokeWidth={1.8} />
                  ) : (
                    <Bot className="h-3 w-3" strokeWidth={1.8} />
                  )}
                  {isCritic
                    ? t(($) => $.overview.detail_panel.critic)
                    : t(($) => $.overview.detail_panel.plugins)}
                </span>
                <h3 className="mt-2.5 truncate text-base font-semibold text-foreground">
                  {node.title}
                </h3>
                {plugin?.description && (
                  <p className="mt-1 text-sm leading-6 text-muted-foreground line-clamp-2">
                    {plugin.description}
                  </p>
                )}
              </div>
              <span className="shrink-0 rounded-full border border-border/70 bg-background p-1.5 text-muted-foreground">
                <ArrowUpRight className="h-4 w-4" strokeWidth={1.9} />
              </span>
            </div>
          </section>

          {/* ── Plugin details ── */}
          {plugin && <PluginSection plugin={plugin} />}

          {/* ── Plugin Bundle info ── */}
          {plugin?.metadata?.bundle && <BundleSection bundle={plugin.metadata.bundle} />}

          {/* ── Agent details ── */}
          {displayEntity && (
            <AgentSection agent={displayEntity} isCritic={isCritic} />
          )}
        </div>

        {/* Bottom action */}
        <div className="sticky bottom-0 border-t bg-background p-3">
          <button
            type="button"
            onClick={onOpenInEditor}
            className="w-full rounded-xl bg-primary py-2.5 text-sm font-medium text-primary-foreground transition-opacity hover:opacity-90"
          >
            {t(($) => $.overview.detail_panel.open_in_editor)}
          </button>
        </div>
      </div>
    </>
  );
}

// ── Sub-components ──

function PluginSection({ plugin }: { plugin: BuiltinPlugin }) {
  const { t } = useT("workflows");

  const fields: [string, string, boolean][] = [
    [t(($) => $.overview.detail_panel.plugin_name), plugin.name, false],
    [t(($) => $.overview.detail_panel.plugin_slug), plugin.slug, false],
    [t(($) => $.overview.detail_panel.plugin_version), `v${plugin.version}`, false],
    [t(($) => $.overview.detail_panel.plugin_category), plugin.category, false],
    [t(($) => $.overview.detail_panel.plugin_description), plugin.description, true],
  ].filter(([, v]) => v) as [string, string, boolean][];

  return (
    <section>
      <h3 className="mb-3 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
        <Package className="h-3.5 w-3.5" strokeWidth={1.8} />
        {t(($) => $.overview.detail_panel.plugins)}
      </h3>
      <dl className="grid grid-cols-2 gap-1.5 text-xs">
        {fields.map(([label, value, fullWidth]) => (
          <div
            key={label}
            className={cn(
              "rounded-lg border border-border/50 bg-background/70 px-3 py-2",
              fullWidth && "col-span-2",
            )}
          >
            <dt className="mb-0.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</dt>
            <dd className="min-w-0 break-words text-foreground">{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

interface BundleInfo {
  skills_count: number;
  agents_count: number;
  commands_count: number;
  hooks_count: number;
  skills_namespaces: string[];
}

function BundleSection({ bundle }: { bundle: BundleInfo }) {
  const { t } = useT("workflows");

  const skillsCount = bundle.skills_count ?? 0;
  const agentsCount = bundle.agents_count ?? 0;
  const namespaces: string[] = bundle.skills_namespaces ?? [];

  return (
    <section>
      <h3 className="mb-3 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
        <Puzzle className="h-3.5 w-3.5" strokeWidth={1.8} />
        {t(($) => $.overview.detail_panel.bundle)}
      </h3>
      <div className="space-y-3">
        <div className="grid grid-cols-2 gap-2 text-xs">
          <div className="rounded-lg border border-border/50 bg-background/70 px-3 py-2.5 text-center">
            <div className="text-lg font-semibold text-foreground">{skillsCount}</div>
            <div className="text-[10px] text-muted-foreground uppercase tracking-wider">
              {t(($) => $.overview.detail_panel.skills_count)}
            </div>
          </div>
          <div className="rounded-lg border border-border/50 bg-background/70 px-3 py-2.5 text-center">
            <div className="text-lg font-semibold text-foreground">{agentsCount}</div>
            <div className="text-[10px] text-muted-foreground uppercase tracking-wider">
              {t(($) => $.overview.detail_panel.agents_count)}
            </div>
          </div>
        </div>

        {namespaces.length > 0 && (
          <div>
            <div className="mb-1.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
              {t(($) => $.overview.detail_panel.skills_list)} ({namespaces.length})
            </div>
            <div className="space-y-1 rounded-lg border border-border/50 bg-background/70 p-2">
              {namespaces.map((ns: string) => {
                const shortName = ns.includes(":") ? ns.split(":").slice(1).join(":") : ns;
                return (
                  <div
                    key={ns}
                    className="truncate rounded px-2 py-1 text-xs text-foreground/80 bg-muted/40"
                    title={ns}
                  >
                    {shortName}
                  </div>
                );
              })}
            </div>
          </div>
        )}
      </div>
    </section>
  );
}

function AgentSection({ agent, isCritic }: { agent: Agent; isCritic: boolean }) {
  const { t } = useT("workflows");

  // [label, value, fullWidth]
  const fields: [string, string, boolean][] = [
    [t(($) => $.overview.detail_panel.agent_name), agent.name, false],
    [t(($) => $.overview.detail_panel.agent_runtime_mode), agent.runtime_mode, false],
    [t(($) => $.overview.detail_panel.agent_runtime), agent.runtime_id || "-", false],
    [t(($) => $.overview.detail_panel.agent_status), agent.status, false],
    [t(($) => $.overview.detail_panel.agent_model), agent.model || "-", false],
    [t(($) => $.overview.detail_panel.agent_thinking_level), agent.thinking_level || "-", false],
    [t(($) => $.overview.detail_panel.agent_visibility), agent.visibility, false],
    [t(($) => $.overview.detail_panel.agent_max_concurrent), String(agent.max_concurrent_tasks), false],
    [t(($) => $.overview.detail_panel.agent_builtin), agent.is_builtin ? "Yes" : "No", false],
    [t(($) => $.overview.detail_panel.agent_description), agent.description, true],
    [t(($) => $.overview.detail_panel.agent_instructions), agent.instructions || "-", true],
    [
      t(($) => $.overview.detail_panel.agent_custom_env),
      Object.keys(agent.custom_env).length > 0
        ? Object.entries(agent.custom_env)
            .map(([k, v]) => `${k}=${v}`)
            .join(", ")
        : "-",
      true,
    ],
    [
      t(($) => $.overview.detail_panel.agent_custom_args),
      agent.custom_args.length > 0 ? agent.custom_args.join(", ") : "-",
      false,
    ],
    [
      t(($) => $.overview.detail_panel.agent_skills),
      agent.skills.length > 0 ? agent.skills.map((s) => s.name).join(", ") : "-",
      false,
    ],
  ].filter(([, v]) => v !== undefined) as [string, string, boolean][];

  return (
    <section>
      <h3 className="mb-3 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
        <Wrench className="h-3.5 w-3.5" strokeWidth={1.8} />
        {isCritic
          ? t(($) => $.overview.detail_panel.critic)
          : t(($) => $.overview.detail_panel.worker)}
      </h3>
      <dl className="grid grid-cols-2 gap-1.5 text-xs">
        {fields.map(([label, value, fullWidth]) => (
          <div
            key={label}
            className={cn(
              "rounded-lg border border-border/50 bg-background/70 px-3 py-2",
              fullWidth && "col-span-2",
            )}
          >
            <dt className="mb-0.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</dt>
            <dd className="min-w-0 break-words text-foreground">{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}
