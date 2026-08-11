"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Bot, Loader2, Lock, ShieldCheck, Users } from "lucide-react";
import type { A2aInvocationMode, Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { ActorAvatar } from "../../../common/actor-avatar";
import { useT } from "../../../i18n";

export type A2aInvocationChange = {
  a2a_invocation_mode: A2aInvocationMode;
  a2a_invocation_grants: string[];
};

function sameGrants(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((id) => b.includes(id));
}

/**
 * Draft-first editor for the independent A2A invocation axis (NEX-24),
 * orthogonal to the member-visibility access picker. The owner picks a mode
 * (not enabled = `default` / any agent / squad leaders / specific agents);
 * for `specific_agents` they also multi-select the whitelist. Nothing persists
 * until the owner saves. Non-owners get a read-only summary.
 *
 * Four-value model: `default` is the "not enabled" / restore-default option;
 * there is deliberately no empty-string value.
 */
export function A2aInvocationPicker({
  mode,
  grants,
  agentId,
  agents,
  canEdit = true,
  onDirtyChange,
  onChange,
}: {
  mode: A2aInvocationMode | undefined;
  grants: string[] | undefined;
  /** The agent being configured — excluded from the whitelist candidates. */
  agentId: string;
  /** Workspace agents — candidates for the `specific_agents` whitelist. */
  agents: Agent[];
  canEdit?: boolean;
  onDirtyChange?: (dirty: boolean) => void;
  onChange?: (next: A2aInvocationChange) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const { t: tc } = useT("common");

  const persistedMode: A2aInvocationMode = mode ?? "default";
  const persistedGrants = useMemo(() => grants ?? [], [grants]);
  const [draftMode, setDraftMode] = useState<A2aInvocationMode>(persistedMode);
  const [draftGrants, setDraftGrants] = useState<string[]>(persistedGrants);
  const [saving, setSaving] = useState(false);

  // Only reset the draft when the PERSISTED values actually change by value
  // (same reference-guard pattern as AccessPicker).
  const prevPersistedModeRef = useRef(persistedMode);
  const prevPersistedGrantsRef = useRef(persistedGrants);
  useEffect(() => {
    const modeChanged = persistedMode !== prevPersistedModeRef.current;
    const grantsChanged = !sameGrants(
      persistedGrants,
      prevPersistedGrantsRef.current,
    );
    if (modeChanged || grantsChanged) {
      setDraftMode(persistedMode);
      setDraftGrants(persistedGrants);
      prevPersistedModeRef.current = persistedMode;
      prevPersistedGrantsRef.current = persistedGrants;
    }
  }, [persistedMode, persistedGrants]);

  // Candidates exclude the agent itself — an agent doesn't need to be in its
  // own A2A whitelist (the owner path and normal invocation rules already
  // cover the self case, and self-invocation is not an A2A collaboration).
  const candidates = useMemo(
    () =>
      agents
        .filter((a) => a.id !== agentId && !a.archived_at)
        .sort((a, b) => a.name.localeCompare(b.name)),
    [agents, agentId],
  );

  const sameGrantsAsPersisted = sameGrants(draftGrants, persistedGrants);
  const dirty =
    draftMode !== persistedMode ||
    (draftMode === "specific_agents" && !sameGrantsAsPersisted);

  useEffect(() => {
    onDirtyChange?.(dirty);
    return () => onDirtyChange?.(false);
  }, [dirty, onDirtyChange]);

  const draftChange = useMemo<A2aInvocationChange>(() => {
    if (draftMode !== "specific_agents") {
      return { a2a_invocation_mode: draftMode, a2a_invocation_grants: [] };
    }
    return {
      a2a_invocation_mode: draftMode,
      a2a_invocation_grants: draftGrants,
    };
  }, [draftMode, draftGrants]);

  const save = async () => {
    if (!onChange || saving) return;
    setSaving(true);
    try {
      await onChange(draftChange);
    } finally {
      setSaving(false);
    }
  };

  const toggleCandidate = (id: string, checked: boolean) => {
    setDraftGrants((current) => {
      const next = new Set(current);
      if (checked) next.add(id);
      else next.delete(id);
      return Array.from(next);
    });
  };

  if (!canEdit) {
    const summaryLabel =
      persistedMode === "any_agent"
        ? t(($) => $.a2a.trigger_any_agent)
        : persistedMode === "squad_leaders"
          ? t(($) => $.a2a.trigger_squad_leaders)
          : persistedMode === "specific_agents"
            ? persistedGrants.length > 0
              ? t(($) => $.a2a.trigger_specific_agents_count, {
                  count: persistedGrants.length,
                })
              : t(($) => $.a2a.trigger_specific_agents)
            : t(($) => $.a2a.trigger_not_enabled);

    return (
      <div
        className="flex items-start gap-3 px-4 py-4"
        aria-label={t(($) => $.a2a.owner_only_readonly)}
        data-testid="a2a-readonly"
      >
        <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
          <ShieldCheck className="size-4" aria-hidden="true" />
        </span>
        <div className="min-w-0">
          <p className="text-body font-medium">{summaryLabel}</p>
          <p className="mt-0.5 text-caption leading-5 text-muted-foreground">
            {t(($) => $.a2a.owner_only_readonly)}
          </p>
        </div>
      </div>
    );
  }

  return (
    <fieldset>
      <legend className="sr-only">{t(($) => $.a2a.tooltip)}</legend>

      <div className="divide-y divide-surface-border">
        <A2aChoice
          name="agent-a2a-mode"
          value="default"
          icon={Lock}
          title={t(($) => $.a2a.not_enabled_title)}
          description={t(($) => $.a2a.not_enabled_desc)}
          selected={draftMode === "default"}
          onSelect={() => setDraftMode("default")}
        />
        <A2aChoice
          name="agent-a2a-mode"
          value="any_agent"
          icon={Users}
          title={t(($) => $.a2a.any_agent_title)}
          description={t(($) => $.a2a.any_agent_desc)}
          selected={draftMode === "any_agent"}
          onSelect={() => setDraftMode("any_agent")}
        />
        <A2aChoice
          name="agent-a2a-mode"
          value="squad_leaders"
          icon={ShieldCheck}
          title={t(($) => $.a2a.squad_leaders_title)}
          description={t(($) => $.a2a.squad_leaders_desc)}
          selected={draftMode === "squad_leaders"}
          onSelect={() => setDraftMode("squad_leaders")}
        />
        <A2aChoice
          name="agent-a2a-mode"
          value="specific_agents"
          icon={Bot}
          title={t(($) => $.a2a.specific_agents_title)}
          description={t(($) => $.a2a.specific_agents_desc)}
          selected={draftMode === "specific_agents"}
          onSelect={() => setDraftMode("specific_agents")}
        />
      </div>

      {draftMode === "specific_agents" ? (
        <div className="border-t border-surface-border bg-muted/20 px-4 py-5 sm:px-6">
          <div>
            {candidates.length === 0 ? (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.a2a.agents_empty)}
              </p>
            ) : (
              <div className="max-h-64 divide-y divide-surface-border overflow-y-auto rounded-lg border bg-background overscroll-contain">
                {candidates.map((candidate) => {
                  const id = `agent-a2a-grantee-${candidate.id}`;
                  return (
                    <div
                      key={candidate.id}
                      className="flex items-center gap-3 px-3 py-3 hover:bg-surface-hover"
                    >
                      <Checkbox
                        id={id}
                        checked={draftGrants.includes(candidate.id)}
                        onCheckedChange={(value) =>
                          toggleCandidate(candidate.id, value === true)
                        }
                      />
                      <label
                        htmlFor={id}
                        className="flex min-w-0 flex-1 cursor-pointer items-center gap-3"
                      >
                        <ActorAvatar
                          actorType="agent"
                          actorId={candidate.id}
                          size="sm"
                        />
                        <span className="min-w-0 flex-1 truncate text-body">
                          {candidate.name}
                        </span>
                      </label>
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          {draftMode === "specific_agents" && draftGrants.length === 0 ? (
            <p className="mt-3 text-caption text-destructive" role="alert">
              {t(($) => $.a2a.specific_required)}
            </p>
          ) : null}
        </div>
      ) : null}

      <div className="flex justify-end border-t border-surface-border px-4 py-3.5">
        <Button
          type="button"
          onClick={() => void save()}
          disabled={
            !onChange ||
            !dirty ||
            saving ||
            (draftMode === "specific_agents" && draftGrants.length === 0)
          }
        >
          {saving ? (
            <Loader2
              className="size-4 animate-spin motion-reduce:animate-none"
              aria-hidden="true"
            />
          ) : null}
          {tc(($) => $.save)}
        </Button>
      </div>
    </fieldset>
  );
}

function A2aChoice({
  name,
  value,
  icon: Icon,
  title,
  description,
  selected,
  onSelect,
}: {
  name: string;
  value: string;
  icon: typeof Lock;
  title: string;
  description: string;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <label className="flex min-h-16 cursor-pointer items-start gap-3 px-4 py-3.5 transition-colors hover:bg-surface-hover">
      <input
        type="radio"
        name={name}
        value={value}
        checked={selected}
        onChange={onSelect}
        className="mt-2 size-4 shrink-0 accent-foreground"
      />
      <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
        <Icon className="size-4" aria-hidden="true" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-body font-medium">{title}</span>
        <span className="mt-0.5 block text-caption leading-5 text-muted-foreground">
          {description}
        </span>
      </span>
    </label>
  );
}
