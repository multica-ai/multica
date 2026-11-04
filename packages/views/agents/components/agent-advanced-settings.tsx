"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime, MemberWithUser } from "@multica/core/types";
import { providerSupportsMcpConfig } from "@multica/core/agents";
import { useFeatureEnabled } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { larkInstallationsOptions } from "@multica/core/lark";
import { slackInstallationsOptions } from "@multica/core/slack";
import { dingtalkInstallationsOptions } from "@multica/core/dingtalk";
import { wecomInstallationsOptions } from "@multica/core/wecom";
import { telegramInstallationsOptions } from "@multica/core/telegram";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { SkillsTab } from "./tabs/skills-tab";
import { EnvTab } from "./tabs/env-tab";
import { CustomArgsTab } from "./tabs/custom-args-tab";
import { McpConfigTab } from "./tabs/mcp-config-tab";
import { AgentMcpTab } from "./tabs/agent-mcp-tab";
import { IntegrationsTab } from "./tabs/integrations-tab";
import { QoderAgentTab } from "./tabs/qoder-agent-tab";
import { RuntimeConfigTab } from "./tabs/runtime-config-tab";
import { AgentDetailInspector } from "./agent-detail-inspector";
import { AgentAccessSettings } from "./agent-access-settings";
import { useT } from "../../i18n";

export type AdvancedSection =
  | "general"
  | "access"
  | "env"
  | "custom_args"
  | "runtime_config"
  | "skills"
  | "mcp_config"
  | "composio_mcp"
  | "integrations";

type SectionLabelKey =
  | "general"
  | "access"
  | "environment"
  | "custom_args"
  | "runtime_config"
  | "qoder_agent"
  | "skills"
  | "mcp_config"
  | "composio_mcp"
  | "integrations";

type SectionDef = { id: AdvancedSection; labelKey: SectionLabelKey };

const SETTINGS_SECTIONS: SectionDef[] = [
  { id: "general", labelKey: "general" },
  { id: "access", labelKey: "access" },
  { id: "env", labelKey: "environment" },
  { id: "custom_args", labelKey: "custom_args" },
  { id: "runtime_config", labelKey: "runtime_config" },
];

const CAPABILITY_SECTIONS: SectionDef[] = [
  { id: "skills", labelKey: "skills" },
  { id: "mcp_config", labelKey: "mcp_config" },
  { id: "composio_mcp", labelKey: "composio_mcp" },
  { id: "integrations", labelKey: "integrations" },
];

const ADVANCED_SECTIONS = new Set<string>(
  [...SETTINGS_SECTIONS, ...CAPABILITY_SECTIONS].map((section) => section.id),
);

export function isAdvancedSection(value: string | null): value is AdvancedSection {
  return value !== null && ADVANCED_SECTIONS.has(value);
}

interface AgentAdvancedSettingsProps {
  agent: Agent;
  runtime: AgentRuntime | null;
  runtimes: AgentRuntime[];
  members: MemberWithUser[];
  onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
  currentUserId: string | null;
  canEdit: boolean;
  /** The open section, or `null` when the dialog is closed. */
  section: AdvancedSection | null;
  onSectionChange: (section: AdvancedSection | null) => void;
}

/**
 * Every agent setting that is not a section of the page or a property in its
 * panel. Sections the agent's runtime cannot use are not offered, and a
 * section with unsaved edits asks before it is left or closed.
 */
export function AgentAdvancedSettings({
  agent,
  runtime,
  runtimes,
  members,
  onUpdate,
  currentUserId,
  canEdit,
  section,
  onSectionChange,
}: AgentAdvancedSettingsProps) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const composioMCPAppsEnabled = useFeatureEnabled(
    COMPOSIO_MCP_APPS_FLAG,
    false,
  );
  const [dirty, setDirty] = useState(false);
  // `undefined` = no pending move; `null` = a pending close.
  const [pending, setPending] = useState<AdvancedSection | null | undefined>(
    undefined,
  );

  const { data: larkListing } = useQuery({
    ...larkInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const { data: slackListing } = useQuery({
    ...slackInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const { data: dingtalkListing } = useQuery({
    ...dingtalkInstallationsOptions(wsId),
  });
  const { data: wecomListing } = useQuery({
    ...wecomInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const { data: telegramListing } = useQuery({
    ...telegramInstallationsOptions(wsId),
    enabled: !!wsId,
  });

  const integrationsConfigured =
    larkListing?.configured === true ||
    slackListing?.configured === true ||
    dingtalkListing?.configured === true ||
    wecomListing?.configured === true ||
    telegramListing?.configured === true;

  // The Qoder bridge fails every task carrying skills, MCP, connected apps,
  // env, args or a model override, and runs issues only (no chat). Those
  // editors are not offered; the agent in Qoder Cloud Agents owns that configuration.
  const qoderRuntime = runtime?.provider === "qoder_cloud";

  const settingsSections = useMemo(
    () =>
      SETTINGS_SECTIONS.filter((item) => {
        if (qoderRuntime) return item.id !== "env" && item.id !== "custom_args";
        // Env is the only section backed by a secret-bearing endpoint.
        // GET/PUT /api/agents/{id}/env admits the agent owner or a workspace
        // owner/admin (MUL-5438) — the same rule `canEdit` encodes — so
        // showing it to anyone else guarantees a 403 on "Reveal & edit".
        // The server stays the boundary; this only removes a dead entry point.
        if (item.id === "env") return canEdit;
        if (item.id === "runtime_config") return runtime?.provider === "openclaw";
        return true;
      }).map((item) =>
        qoderRuntime && item.id === "runtime_config"
          ? { ...item, labelKey: "qoder_agent" as const }
          : item,
      ),
    [canEdit, qoderRuntime, runtime?.provider],
  );

  const capabilitySections = useMemo(() => {
    if (qoderRuntime) return [];
    const showMcp = runtime ? providerSupportsMcpConfig(runtime.provider) : true;
    const showComposioMcp =
      composioMCPAppsEnabled &&
      !!currentUserId &&
      !!agent.owner_id &&
      agent.owner_id === currentUserId;
    return CAPABILITY_SECTIONS.filter((item) => {
      if (item.id === "mcp_config") return showMcp;
      if (item.id === "composio_mcp") return showComposioMcp;
      if (item.id === "integrations") return integrationsConfigured;
      return true;
    });
  }, [
    agent.owner_id,
    composioMCPAppsEnabled,
    currentUserId,
    integrationsConfigured,
    qoderRuntime,
    runtime,
  ]);

  const visible = [...settingsSections, ...capabilitySections];
  // A link to a section this agent doesn't offer lands on General.
  const active =
    visible.find((item) => item.id === section) ?? visible[0] ?? SETTINGS_SECTIONS[0]!;

  const request = (next: AdvancedSection | null) => {
    if (next === active.id) return;
    if (dirty) {
      setPending(next);
      return;
    }
    onSectionChange(next);
  };

  const confirmDiscard = () => {
    if (pending === undefined) return;
    setDirty(false);
    onSectionChange(pending);
    setPending(undefined);
  };

  const save = (updates: Record<string, unknown>) => onUpdate(agent.id, updates);

  const groups = [
    { label: t(($) => $.tabs.settings), items: settingsSections },
    { label: t(($) => $.tabs.capabilities), items: capabilitySections },
  ].filter((group) => group.items.length > 0);

  return (
    <>
      <Dialog
        open={section !== null}
        onOpenChange={(open) => {
          if (!open) request(null);
        }}
      >
        <DialogContent className="flex h-[min(85dvh,52rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl">
          <DialogHeader className="shrink-0 gap-1 border-b px-5 py-4 pr-12">
            <DialogTitle>{t(($) => $.detail.advanced_settings)}</DialogTitle>
            <DialogDescription className="truncate">{agent.name}</DialogDescription>
          </DialogHeader>

          {/* Sidebar on md+ (nav pinned, content scrolls); below md the nav
              is a horizontal strip above the content. */}
          <div className="flex min-h-0 flex-1 flex-col md:flex-row">
            <nav
              className="shrink-0 overflow-x-auto border-b px-3 py-2 md:w-48 md:overflow-y-auto md:border-b-0 md:border-r md:py-3"
              aria-label={t(($) => $.tabs.section_navigation_aria)}
            >
              <div className="flex w-max min-w-full items-center gap-1 md:w-full md:flex-col md:items-stretch md:gap-4">
                {groups.map((group) => (
                  <div
                    key={group.label}
                    role="tablist"
                    aria-orientation="vertical"
                    aria-label={group.label}
                    className="flex items-center gap-1 md:flex-col md:items-stretch md:gap-0.5"
                  >
                    <span className="hidden px-2.5 pb-1 text-micro font-medium text-muted-foreground md:block">
                      {group.label}
                    </span>
                    {group.items.map((item) => {
                      const selected = active.id === item.id;
                      return (
                        <button
                          key={item.id}
                          type="button"
                          role="tab"
                          aria-selected={selected}
                          onClick={() => request(item.id)}
                          className={cn(
                            "flex h-8 shrink-0 items-center rounded-md px-2.5 text-left text-caption transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:w-full",
                            selected
                              ? "bg-surface-selected text-surface-selected-foreground hover:bg-surface-selected"
                              : "text-muted-foreground hover:bg-surface-hover hover:text-foreground",
                          )}
                        >
                          {t(($) => $.tabs[item.labelKey])}
                        </button>
                      );
                    })}
                  </div>
                ))}
              </div>
            </nav>

            <section role="tabpanel" className="min-w-0 flex-1 overflow-y-auto">
              {/* General is wider: its profile sits beside a preview of the
                  new chat it produces. */}
              <div
                className={cn(
                  "w-full p-5 sm:p-6",
                  active.id === "general" ? "max-w-5xl" : "max-w-3xl",
                )}
              >
                <h2 className="text-title-sm font-medium text-balance">
                  {t(($) => $.tabs[active.labelKey])}
                </h2>
                <div className="mt-6">
                  {active.id === "general" && (
                    <AgentDetailInspector
                      agent={agent}
                      runtime={runtime}
                      runtimes={runtimes}
                      members={members}
                      currentUserId={currentUserId}
                      canEdit={canEdit}
                      onUpdate={onUpdate}
                    />
                  )}
                  {active.id === "access" && (
                    <AgentAccessSettings
                      agent={agent}
                      members={members}
                      currentUserId={currentUserId}
                      onDirtyChange={setDirty}
                      onUpdate={onUpdate}
                    />
                  )}
                  {active.id === "env" && (
                    <EnvTab agent={agent} onDirtyChange={setDirty} />
                  )}
                  {active.id === "custom_args" && (
                    <CustomArgsTab
                      agent={agent}
                      runtimeDevice={runtime ?? undefined}
                      onSave={save}
                      onDirtyChange={setDirty}
                    />
                  )}
                  {active.id === "runtime_config" &&
                    (qoderRuntime ? (
                      <QoderAgentTab
                        canEdit={canEdit}
                        key={agent.id}
                        agent={agent}
                        onSave={save}
                        onDirtyChange={setDirty}
                      />
                    ) : (
                      <RuntimeConfigTab
                        agent={agent}
                        onSave={save}
                        onDirtyChange={setDirty}
                      />
                    ))}
                  {active.id === "skills" && (
                    <SkillsTab
                      agent={agent}
                      runtime={runtime}
                      currentUserId={currentUserId}
                      canEdit={canEdit}
                    />
                  )}
                  {active.id === "mcp_config" && (
                    <McpConfigTab
                      agent={agent}
                      runtime={runtime}
                      currentUserId={currentUserId}
                      canEdit={canEdit}
                      onSave={save}
                      onDirtyChange={setDirty}
                    />
                  )}
                  {active.id === "composio_mcp" && <AgentMcpTab agent={agent} />}
                  {active.id === "integrations" && (
                    <IntegrationsTab agent={agent} />
                  )}
                </div>
              </div>
            </section>
          </div>
        </DialogContent>
      </Dialog>

      {pending !== undefined && (
        <AlertDialog
          open
          onOpenChange={(open) => {
            if (!open) setPending(undefined);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t(($) => $.tabs.discard_dialog_title)}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.tabs.discard_dialog_description)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t(($) => $.tabs.discard_keep)}</AlertDialogCancel>
              <AlertDialogAction variant="destructive" onClick={confirmDiscard}>
                {t(($) => $.tabs.discard_confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </>
  );
}
