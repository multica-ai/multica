"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { squadBriefsOptions } from "@multica/core/workspace/queries";
import {
  SQUAD_HANDOFF_DAILY_LIMIT,
  SQUAD_HANDOFF_ROUND_LIMIT,
  type Squad,
  type SquadHandoffMode,
} from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { Bot } from "@multica/ui/icons";
import { ActorAvatar } from "../../common/actor-avatar";
import {
  ObjectSection,
  ObjectSectionCard,
  ObjectSectionEmpty,
} from "../../layout/object-page";
import { useT } from "../../i18n";

/**
 * How work moves between the squad's members. Read-only for people who cannot
 * edit the squad; the guardrails of the direct mode are fixed, so they are
 * stated rather than offered as settings.
 */
export function SquadHandoffSection({
  squad,
  leaderName,
  canManage,
  pending,
  onChange,
}: {
  squad: Squad;
  leaderName: string;
  canManage: boolean;
  pending: boolean;
  onChange: (mode: SquadHandoffMode) => void;
}) {
  const { t } = useT("squads");
  const mode = squad.handoff_mode ?? "leader";
  const select = (next: SquadHandoffMode) => {
    if (next !== mode) onChange(next);
  };

  return (
    <ObjectSection title={t(($) => $.handoff.section_title)}>
      <ObjectSectionCard>
        <div role="radiogroup" aria-label={t(($) => $.handoff.section_title)} className="divide-y">
          <HandoffModeOption
            title={t(($) => $.handoff.mode_leader_title)}
            description={t(($) => $.handoff.mode_leader_description, { leader: leaderName })}
            selected={mode === "leader"}
            disabled={!canManage || pending}
            onSelect={() => select("leader")}
          />
          <HandoffModeOption
            title={t(($) => $.handoff.mode_direct_title)}
            description={t(($) => $.handoff.mode_direct_description)}
            selected={mode === "direct"}
            disabled={!canManage || pending}
            onSelect={() => select("direct")}
          >
            {mode === "direct" && (
              <span className="mt-2 flex flex-wrap gap-1.5">
                {[
                  t(($) => $.handoff.guard_rounds, { limit: SQUAD_HANDOFF_ROUND_LIMIT }),
                  t(($) => $.handoff.guard_daily, { limit: SQUAD_HANDOFF_DAILY_LIMIT }),
                  t(($) => $.handoff.guard_failed),
                  t(($) => $.handoff.guard_no_next),
                ].map((guard) => (
                  <span
                    key={guard}
                    className="rounded-xs bg-muted px-1.5 py-0.5 text-caption text-muted-foreground"
                  >
                    {guard}
                  </span>
                ))}
              </span>
            )}
          </HandoffModeOption>
        </div>
      </ObjectSectionCard>
    </ObjectSection>
  );
}

function HandoffModeOption({
  title,
  description,
  selected,
  disabled,
  onSelect,
  children,
}: {
  title: string;
  description: string;
  selected: boolean;
  disabled: boolean;
  onSelect: () => void;
  children?: React.ReactNode;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      disabled={disabled}
      onClick={onSelect}
      className={cn(
        "flex w-full items-start gap-3 px-3 py-2.5 text-left transition-colors",
        !disabled && "hover:bg-accent/50",
        disabled && "cursor-default",
      )}
    >
      <span
        aria-hidden="true"
        className={cn(
          "mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border",
          selected ? "border-primary" : "border-input",
        )}
      >
        {selected && <span className="size-2 rounded-full bg-primary" />}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-body font-medium">{title}</span>
        <span className="mt-0.5 block text-caption text-muted-foreground">{description}</span>
        {children}
      </span>
    </button>
  );
}

/**
 * The squad context each agent of the squad actually gets on its runs, as the
 * server renders it. Shown to the people who can edit the squad.
 */
export function SquadBriefsSection({
  squadId,
  getEntityName,
}: {
  squadId: string;
  getEntityName: (type: string, id: string) => string;
}) {
  const { t } = useT("squads");
  const wsId = useWorkspaceId();
  const { data, isPending } = useQuery(squadBriefsOptions(wsId, squadId));
  const briefs = data?.briefs ?? [];
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const selected =
    briefs.find((b) => b.member_id === selectedId) ??
    briefs.find((b) => !b.is_leader) ??
    briefs[0];

  return (
    <ObjectSection
      title={t(($) => $.handoff.brief_title)}
      // The section's caption slot, used here for where the brief goes.
      count={t(($) => $.handoff.brief_hint)}
    >
      {isPending ? (
        <Skeleton className="h-40 w-full rounded-lg" />
      ) : selected ? (
        <ObjectSectionCard>
          <div role="tablist" className="flex items-center gap-1 overflow-x-auto bg-muted/50 px-2 py-1.5">
            {briefs.map((b) => {
              const active = b.member_id === selected.member_id;
              return (
                <button
                  key={b.member_id}
                  type="button"
                  role="tab"
                  aria-selected={active}
                  onClick={() => setSelectedId(b.member_id)}
                  className={cn(
                    "flex h-7 shrink-0 items-center gap-1.5 rounded-md px-2 text-label transition-colors",
                    active
                      ? "bg-background font-medium text-foreground shadow-xs"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  <ActorAvatar actorType="agent" actorId={b.member_id} size="xs" />
                  {getEntityName("agent", b.member_id)}
                </button>
              );
            })}
          </div>
          <pre
            role="tabpanel"
            className="max-h-96 overflow-auto whitespace-pre-wrap break-words px-3 py-2.5 font-mono text-caption text-foreground"
          >
            {selected.brief}
          </pre>
        </ObjectSectionCard>
      ) : (
        <ObjectSectionEmpty icon={Bot} description={t(($) => $.handoff.brief_empty)} />
      )}
    </ObjectSection>
  );
}
