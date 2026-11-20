"use client";

import { useEffect, useState } from "react";
import { toast } from "sonner";
import type { AgentRuntime } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  AGENT_MAX_CONCURRENT_TASKS_MAX,
  AGENT_MAX_CONCURRENT_TASKS_MIN,
} from "@multica/core/agents";
import { useUpdateRuntime } from "@multica/core/runtimes/mutations";
import { isImeComposing } from "@multica/core/utils";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";

/**
 * Parses the field draft. Empty means "no limit" (null); otherwise an integer
 * in the same range as an agent's own limit. Returns undefined for anything
 * else so the caller can revert the draft.
 */
export function parseRuntimeConcurrencyDraft(draft: string): number | null | undefined {
  const trimmed = draft.trim();
  if (trimmed === "") return null;
  const next = Number(trimmed);
  if (
    !Number.isInteger(next) ||
    next < AGENT_MAX_CONCURRENT_TASKS_MIN ||
    next > AGENT_MAX_CONCURRENT_TASKS_MAX
  ) {
    return undefined;
  }
  return next;
}

/**
 * Runtime-wide concurrency cap (GitHub #9626): bounds the combined running
 * tasks of every agent bound to this runtime, e.g. several agents sharing
 * one local GPU. Empty means no runtime cap.
 */
export function RuntimeConcurrencyField({
  runtime,
  canEdit,
}: {
  runtime: AgentRuntime;
  canEdit: boolean;
}) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const updateRuntime = useUpdateRuntime(wsId);
  const value = runtime.max_concurrent_tasks ?? null;
  const toDraft = (v: number | null) => (v === null ? "" : String(v));
  const [draft, setDraft] = useState(toDraft(value));

  useEffect(() => setDraft(toDraft(value)), [value]);

  if (!canEdit) {
    return (
      <span className="font-mono text-caption tabular-nums text-muted-foreground">
        {value ?? t(($) => $.detail.concurrency_none)}
      </span>
    );
  }

  const commit = () => {
    const next = parseRuntimeConcurrencyDraft(draft);
    if (next === undefined) {
      setDraft(toDraft(value));
      return;
    }
    if (next === value) return;
    updateRuntime.mutate(
      { runtimeId: runtime.id, patch: { max_concurrent_tasks: next } },
      {
        onError: (err) => {
          setDraft(toDraft(value));
          toast.error(
            err instanceof Error && err.message
              ? err.message
              : t(($) => $.detail.toast_concurrency_failed),
          );
        },
      },
    );
  };

  return (
    <div>
      <Input
        id="runtime-concurrency"
        type="number"
        name="runtime-concurrency"
        autoComplete="off"
        inputMode="numeric"
        min={AGENT_MAX_CONCURRENT_TASKS_MIN}
        max={AGENT_MAX_CONCURRENT_TASKS_MAX}
        placeholder={t(($) => $.detail.concurrency_none)}
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={commit}
        onKeyDown={(event) => {
          if (isImeComposing(event)) return;
          if (event.key === "Enter") {
            event.preventDefault();
            commit();
          }
        }}
        disabled={updateRuntime.isPending}
        aria-label={t(($) => $.detail.concurrency_title)}
        className="font-mono tabular-nums"
      />
      <p className="mt-1 text-caption text-muted-foreground">
        {t(($) => $.detail.concurrency_hint, {
          min: AGENT_MAX_CONCURRENT_TASKS_MIN,
          max: AGENT_MAX_CONCURRENT_TASKS_MAX,
        })}
      </p>
    </div>
  );
}
