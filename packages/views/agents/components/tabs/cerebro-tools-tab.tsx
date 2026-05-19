"use client";

// CEREBRO-PATCH(agent-tools-tab): JEH-1710 bid 4 — agent override UI on top of
// runtime-level tool inventory. Replaces the previous AgentTool toggle list
// (which read agent_tool_grant directly) with the runtime-default + per-agent
// override model. The body lives in @multica/cerebro-runtime/views so the
// upstream-zone footprint stays minimal — this wrapper just decides admin gate
// + resolves the runtime name shown in the inherit banner.

import { useQuery } from "@tanstack/react-query";
import { Info } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import { memberListOptions } from "@multica/core/workspace/queries";
import { AgentToolsCard } from "@multica/cerebro-runtime/views";
import { useT } from "../../../i18n";

const runtimeListKey = (wsId: string) =>
  ["workspace", "agent-runtimes", wsId] as const;

export function CerebroToolsTab({ agent }: { agent: Agent }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const isLocalAgent = agent.runtime_mode === "local";

  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId && !isLocalAgent,
  });
  const { data: runtimes = [] } = useQuery({
    queryKey: runtimeListKey(wsId),
    queryFn: () => api.listRuntimes({ workspace_id: wsId }),
    enabled: !!wsId && !isLocalAgent,
  });

  if (isLocalAgent) {
    return (
      <div className="space-y-4">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.tools.intro)}
        </p>
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12">
          <Info className="h-8 w-8 text-muted-foreground/40" />
          <p className="mt-3 text-sm text-muted-foreground">
            {t(($) => $.tab_body.tools.local_empty_title)}
          </p>
          <p className="mt-1 max-w-xs text-center text-xs text-muted-foreground">
            {t(($) => $.tab_body.tools.local_agent_note)}
          </p>
        </div>
      </div>
    );
  }

  const currentMember = user ? members.find((m) => m.user_id === user.id) : null;
  const isAdmin = currentMember
    ? currentMember.role === "owner" || currentMember.role === "admin"
    : false;
  const runtime = runtimes.find((r) => r.id === agent.runtime_id);

  return (
    <AgentToolsCard
      agent={agent}
      canEdit={isAdmin}
      runtimeName={runtime?.name}
    />
  );
}
