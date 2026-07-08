"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, Loader2, Lock, Plug } from "lucide-react";
import { toast } from "sonner";
import type { Agent, ComposioToolkit } from "@multica/core/types";
import { useUpdateAgentAllowlist } from "@multica/core/agents";
import { useFeatureEnabled } from "@multica/core/config";
import {
  composioConnectionsOptions,
  composioToolkitsOptions,
} from "@multica/core/composio";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { useWorkspacePaths } from "@multica/core/paths";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { ComposioToolkitLogo } from "../../../common/composio-toolkit-logo";
import { AppLink } from "../../../navigation";
import { useT } from "../../../i18n";

/**
 * Creator-only MCP tab on the agent detail page (MUL-3870). Lets the agent
 * owner pick which of *their own* active Composio connections this agent may
 * mount as MCP servers — the selection is written to
 * `agent.composio_toolkit_allowlist`. At dispatch the overlay is mounted for
 * ANY run that passes the agent's invocation permission and always uses the
 * agent OWNER's Composio connection (MUL-3963) — it is no longer gated on the
 * run originator being the owner. That is why sharing the agent (public_to)
 * surfaces the warning banner below: everyone who can invoke the agent can
 * drive these apps through it.
 *
 * Visibility is enforced by the parent (the tab entry isn't rendered unless
 * `agent.owner_id === viewer.id`), so this component assumes the owner. It
 * still renders a defensive "hidden" state if the server redacted the
 * allowlist, and reads the checked state straight from the agent prop so the
 * optimistic cache write in `useUpdateAgentAllowlist` flips each box
 * instantly.
 */
export function AgentMcpTab({ agent }: { agent: Agent }) {
  const { t } = useT("agents");
  const paths = useWorkspacePaths();
  const composioEnabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false);
  const updateAllowlist = useUpdateAgentAllowlist(agent.id);

  const connectionsQuery = useQuery({
    ...composioConnectionsOptions(),
    enabled: composioEnabled,
  });
  const toolkitsQuery = useQuery({
    ...composioToolkitsOptions(),
    enabled: composioEnabled,
  });

  // Toolkit metadata (name / logo) keyed by slug, so each connection row can
  // render a friendly label instead of the bare slug. The catalog is a
  // best-effort enrichment — a missing entry just falls back to the slug.
  const toolkitBySlug = useMemo(() => {
    const m = new Map<string, ComposioToolkit>();
    for (const tk of toolkitsQuery.data ?? []) m.set(tk.slug, tk);
    return m;
  }, [toolkitsQuery.data]);

  // Only ACTIVE connections are selectable — an expired / revoked connection
  // can't back an MCP mount, so offering its checkbox would be a dead toggle.
  // Dedupe by slug (a user could in theory hold two rows for one toolkit).
  const activeSlugs = useMemo(() => {
    const seen = new Set<string>();
    const out: string[] = [];
    for (const c of connectionsQuery.data ?? []) {
      if (c.status !== "active") continue;
      if (seen.has(c.toolkit_slug)) continue;
      seen.add(c.toolkit_slug);
      out.push(c.toolkit_slug);
    }
    return out;
  }, [connectionsQuery.data]);

  const allowlist = useMemo(
    () => agent.composio_toolkit_allowlist ?? [],
    [agent.composio_toolkit_allowlist],
  );

  const settingsHref = `${paths.settings()}?tab=integrations`;

  // Composio access warning (MUL-3963). Once an agent is shared, anyone who
  // can invoke it can drive the Composio apps enabled here on the owner's
  // behalf — so surface a heads-up whenever the agent is not private and
  // there's something to enable (or already enabled). Public-to-workspace
  // gets the stronger copy because it opens the apps to every member.
  const isPrivate = agent.permission_mode === "private";
  const isWorkspacePublic =
    agent.permission_mode === "public_to" &&
    (agent.invocation_targets ?? []).some(
      (target) => target.target_type === "workspace",
    );
  const showSharedWarning =
    !isPrivate && (allowlist.length > 0 || activeSlugs.length > 0);

  if (!composioEnabled) {
    return null;
  }

  const handleToggle = (slug: string, checked: boolean) => {
    const set = new Set(allowlist);
    if (checked) set.add(slug);
    else set.delete(slug);
    const next = Array.from(set);
    updateAllowlist.mutate(next, {
      onError: () => toast.error(t(($) => $.tab_body.composio_mcp.save_failed_toast)),
    });
  };

  // Defensive: the tab is owner-gated, so a redacted allowlist should never
  // reach here. If it somehow does (stale cache, future fan-out), show the
  // same "configured but hidden" affordance as the MCP config tab rather than
  // an empty editor that a Save could clobber.
  if (agent.composio_toolkit_allowlist_redacted === true) {
    return (
      <div className="space-y-3">
        <p className="flex items-center gap-2 text-sm font-medium">
          <Lock className="h-3.5 w-3.5 text-muted-foreground" />
          {t(($) => $.tab_body.composio_mcp.redacted_title)}
        </p>
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.composio_mcp.redacted_hint)}
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <p className="text-xs text-muted-foreground">
        {t(($) => $.tab_body.composio_mcp.subtitle)}
      </p>

      {showSharedWarning && (
        <div
          role="alert"
          className="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-400"
        >
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>
            {isWorkspacePublic
              ? t(($) => $.tab_body.composio_mcp.workspace_warning)
              : t(($) => $.tab_body.composio_mcp.shared_warning)}
          </span>
        </div>
      )}

      {connectionsQuery.isLoading ? (
        <p className="text-sm text-muted-foreground">
          {t(($) => $.tab_body.composio_mcp.loading)}
        </p>
      ) : connectionsQuery.isError ? (
        <p className="text-sm text-destructive">
          {t(($) => $.tab_body.composio_mcp.load_failed)}
        </p>
      ) : activeSlugs.length === 0 ? (
        <div className="space-y-2 rounded-lg border border-dashed p-6 text-center">
          <p className="text-sm font-medium">
            {t(($) => $.tab_body.composio_mcp.empty_title)}
          </p>
          <p className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.composio_mcp.empty_hint)}
          </p>
          <AppLink
            href={settingsHref}
            className="inline-flex items-center gap-1.5 text-xs font-medium text-primary hover:underline"
          >
            <Plug className="h-3 w-3" />
            {t(($) => $.tab_body.composio_mcp.empty_link_to_settings)}
          </AppLink>
        </div>
      ) : (
        <ul className="divide-y rounded-lg border">
          {activeSlugs.map((slug) => {
            const tk = toolkitBySlug.get(slug);
            const name = tk?.name || slug;
            const checked = allowlist.includes(slug);
            return (
              <li key={slug} className="flex items-center gap-3 p-3">
                <ComposioToolkitLogo slug={slug} name={name} fallbackLogo={tk?.logo} />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{name}</p>
                  <p className="truncate text-[10px] uppercase tracking-wide text-emerald-600">
                    {t(($) => $.tab_body.composio_mcp.connected)}
                  </p>
                </div>
                <Checkbox
                  checked={checked}
                  disabled={updateAllowlist.isPending}
                  onCheckedChange={(value) => handleToggle(slug, value === true)}
                  aria-label={t(($) => $.tab_body.composio_mcp.toggle_aria, {
                    toolkit: name,
                  })}
                />
              </li>
            );
          })}
        </ul>
      )}

      {updateAllowlist.isPending && (
        <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" />
          {t(($) => $.tab_body.composio_mcp.saving)}
        </p>
      )}
    </div>
  );
}
