"use client";

import { useCallback, useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { AgentTaskTokens } from "@multica/core/api/schemas";
import type { Agent } from "@multica/core/types";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { BookOpen } from "lucide-react";
import { toast } from "sonner";
import { useT } from "../../../i18n";
import { openExternal } from "../../../platform";

// taskTokenDocsUrl points at the integration guide — the page a receiving
// system's developer follows to verify these tokens — localized to the viewer's
// language. The docs site is a separate app served at multica.ai/docs, not a
// route on this deployment, so this is absolute like every other doc link in
// the app. The site uses /<lang>/ path prefixes (English has none).
export function taskTokenDocsUrl(lang: string | undefined): string {
  const prefix = lang?.startsWith("zh")
    ? "/zh"
    : lang?.startsWith("ja")
      ? "/ja"
      : lang?.startsWith("ko")
        ? "/ko"
        : "";
  return `https://multica.ai/docs${prefix}/task-identity-tokens`;
}

// Query key is agent-scoped: the catalog is deployment-wide but the enabled
// set is per agent, and they arrive in one response.
export function agentTaskTokensKey(agentId: string) {
  return ["agents", agentId, "task-tokens"] as const;
}

export function TaskTokensTab({ agent }: { agent: Agent }) {
  const { t, i18n } = useT("agents");
  const qc = useQueryClient();

  const { data, isPending, isError } = useQuery({
    queryKey: agentTaskTokensKey(agent.id),
    queryFn: () => api.getAgentTaskTokens(agent.id),
  });

  // Memoized so the `?? []` fallback does not hand out a fresh array on every
  // render, which would reset the toggle callback's identity each time.
  const available = useMemo(() => data?.available ?? [], [data]);
  const enabled = useMemo(() => data?.enabled ?? [], [data]);

  const mutation = useMutation({
    // Scoped so toggles on this agent run one at a time. Parallel mutations
    // could have their responses land out of order, leaving the cache showing
    // a set the server does not hold.
    scope: { id: `agent-task-tokens:${agent.id}` },
    mutationFn: ({ id, checked }: { id: string; checked: boolean }) => {
      // The set is read when the mutation RUNS, not when the box was clicked.
      // The endpoint replaces the whole set, so a payload computed from a
      // snapshot taken before an earlier toggle landed would silently undo it —
      // a lost write on the surface that decides which identities this agent
      // may be issued.
      const current =
        qc.getQueryData<AgentTaskTokens>(agentTaskTokensKey(agent.id))?.enabled ?? [];
      const next = checked
        ? current.includes(id)
          ? current
          : [...current, id]
        : current.filter((e) => e !== id);
      return api.updateAgentTaskTokens(agent.id, next);
    },
    onSuccess: (result) => {
      // The client degrades a response that fails schema validation to the
      // EMPTY shape (agent_id ""). Writing that over the cache would wipe the
      // catalog — hiding the tab mid-edit — and hand the next queued toggle an
      // empty set to build its PUT from, the lost write this tab guards
      // against. The server did apply the change; refetch it instead.
      if (result.agent_id === "") {
        qc.invalidateQueries({ queryKey: agentTaskTokensKey(agent.id) });
      } else {
        qc.setQueryData(agentTaskTokensKey(agent.id), result);
      }
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
  });

  const toggle = useCallback(
    (id: string, checked: boolean) => mutation.mutate({ id, checked }),
    [mutation],
  );

  // "Nothing configured" is a claim about the deployment; a request still in
  // flight or one that failed must not be reported as that fact.
  if (isPending) {
    return null;
  }
  if (isError) {
    return (
      <p className="text-body text-muted-foreground">
        {t(($) => $.tab_body.task_tokens.load_failed)}
      </p>
    );
  }
  if (available.length === 0) {
    return (
      <p className="text-body text-muted-foreground">
        {t(($) => $.tab_body.task_tokens.unconfigured)}
      </p>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-3">
        <p className="text-body text-muted-foreground">
          {t(($) => $.tab_body.task_tokens.description)}
        </p>
        <button
          type="button"
          onClick={() => openExternal(taskTokenDocsUrl(i18n.language))}
          className="inline-flex shrink-0 items-center gap-1.5 text-caption font-medium text-primary underline-offset-2 hover:underline"
          title={t(($) => $.tab_body.task_tokens.docs_link)}
          data-testid="task-tokens-docs-link"
        >
          <BookOpen className="h-4 w-4" />
          {t(($) => $.tab_body.task_tokens.docs_link)}
        </button>
      </div>
      <div className="space-y-3">
        {available.map((tpl) => (
          <label
            key={tpl.id}
            className="flex items-start gap-3 rounded-md border border-border p-3"
          >
            <Checkbox
              checked={enabled.includes(tpl.id)}
              disabled={mutation.isPending}
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
