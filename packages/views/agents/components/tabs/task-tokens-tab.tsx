"use client";

import { useCallback, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { Agent } from "@multica/core/types";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { toast } from "sonner";
import { useT } from "../../../i18n";

// Query key is agent-scoped: the catalog is deployment-wide but the enabled
// set is per agent, and they arrive in one response.
export function agentTaskTokensKey(agentId: string) {
  return ["agents", agentId, "task-tokens"] as const;
}

export function TaskTokensTab({ agent }: { agent: Agent }) {
  const { t } = useT("agents");
  const qc = useQueryClient();
  const [pendingId, setPendingId] = useState<string | null>(null);

  const { data } = useQuery({
    queryKey: agentTaskTokensKey(agent.id),
    queryFn: () => api.getAgentTaskTokens(agent.id),
  });

  // Memoized so the `?? []` fallback does not hand out a fresh array on every
  // render, which would reset the toggle callback's identity each time.
  const available = useMemo(() => data?.available ?? [], [data]);
  const enabled = useMemo(() => data?.enabled ?? [], [data]);

  const mutation = useMutation({
    mutationFn: (next: string[]) => api.updateAgentTaskTokens(agent.id, next),
    onSuccess: (result) => {
      qc.setQueryData(agentTaskTokensKey(agent.id), result);
      toast.success(t(($) => $.tab_body.task_tokens.saved));
    },
    onError: (err) => {
      toast.error(
        err instanceof Error ? err.message : t(($) => $.tab_body.task_tokens.save_failed),
      );
      // The server is the source of truth for what is enabled; refetch rather
      // than leaving the checkboxes showing a change that did not persist.
      qc.invalidateQueries({ queryKey: agentTaskTokensKey(agent.id) });
    },
    onSettled: () => setPendingId(null),
  });

  const toggle = useCallback(
    (id: string, checked: boolean) => {
      setPendingId(id);
      // Send the whole set, matching the endpoint's replace semantics.
      const next = checked ? [...enabled, id] : enabled.filter((e) => e !== id);
      mutation.mutate(next);
    },
    [enabled, mutation],
  );

  if (available.length === 0) {
    return (
      <p className="text-body text-muted-foreground">
        {t(($) => $.tab_body.task_tokens.unconfigured)}
      </p>
    );
  }

  return (
    <div className="space-y-4">
      <p className="text-body text-muted-foreground">
        {t(($) => $.tab_body.task_tokens.description)}
      </p>
      <div className="space-y-3">
        {available.map((tpl) => (
          <label
            key={tpl.id}
            className="flex items-start gap-3 rounded-md border border-border p-3"
          >
            <Checkbox
              checked={enabled.includes(tpl.id)}
              disabled={pendingId === tpl.id}
              onCheckedChange={(checked) => toggle(tpl.id, checked === true)}
              aria-label={tpl.label}
            />
            <span className="min-w-0 flex-1">
              <span className="block text-body font-medium">{tpl.label}</span>
              {tpl.description && (
                <span className="block text-caption text-muted-foreground">
                  {tpl.description}
                </span>
              )}
              <span className="block font-mono text-caption text-muted-foreground">
                {t(($) => $.tab_body.task_tokens.injected_as, { env: tpl.env })}
              </span>
            </span>
          </label>
        ))}
      </div>
    </div>
  );
}
