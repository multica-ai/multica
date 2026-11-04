"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  DEFAULT_AUTONOMY_LEVEL,
  canGovern,
  squadAutonomyOptions,
  useSetSquadAutonomy,
  type AutonomyLevel,
} from "@multica/core/governance";
import { squadListOptions } from "@multica/core/workspace/queries";
import type { Squad } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { PAGE_GUTTER } from "../layout/page-header";
import { useT } from "../i18n";
import { GovernancePoliciesSection } from "./governance-policies-section";
import { AUTONOMY_LEVEL_KEYS } from "./autonomy-levels";

/**
 * The i18n key of the autonomy note (spec 4.3). The note must stay on this tab:
 * it is what stops L3 being read as "unsupervised".
 */
export const AUTONOMY_NOTE_KEY = "autonomy.note";

const LEVELS: readonly AutonomyLevel[] = [0, 1, 2, 3];

/**
 * The Autonomy tab (spec 4.3): what each level allows, each squad's level with
 * the reason of its last change, and the standing approvals (spec 4.4).
 * Owners and admins change levels and policies; everyone else reads them.
 */
export function GovernanceAutonomyTab() {
  const { t } = useT("governance");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const editable = canGovern(role);
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const live = squads.filter((squad) => !squad.archived_at);

  return (
    <div className={cn(PAGE_GUTTER, "flex flex-col gap-8 py-4")}>
      <p className="max-w-prose text-body text-muted-foreground">{t(($) => $.autonomy.note)}</p>

      <section aria-label={t(($) => $.autonomy.levels_heading)} className="flex flex-col gap-2">
        <h2 className="text-heading">{t(($) => $.autonomy.levels_heading)}</h2>
        <ol className="flex max-w-prose flex-col gap-2">
          {LEVELS.map((level) => (
            <li key={level} className="flex flex-col gap-0.5">
              <span className="text-body font-medium">
                {`L${level} ${t(($) => $.autonomy.levels[AUTONOMY_LEVEL_KEYS[level]].name)}`}
              </span>
              <span className="text-body text-muted-foreground">
                {t(($) => $.autonomy.levels[AUTONOMY_LEVEL_KEYS[level]].body)}
              </span>
            </li>
          ))}
        </ol>
        <p className="max-w-prose text-caption text-muted-foreground">{t(($) => $.autonomy.boss_only_note)}</p>
      </section>

      <section aria-label={t(($) => $.autonomy.squads_heading)} className="flex flex-col gap-2">
        <h2 className="text-heading">{t(($) => $.autonomy.squads_heading)}</h2>
        {!editable ? <p className="text-caption text-muted-foreground">{t(($) => $.autonomy.read_only)}</p> : null}
        {live.length === 0 ? (
          <p className="text-body text-muted-foreground">{t(($) => $.autonomy.squads_empty)}</p>
        ) : (
          <ul className="flex flex-col divide-y rounded-md border">
            {live.map((squad) => (
              <SquadAutonomyRow key={squad.id} wsId={wsId} squad={squad} editable={editable} />
            ))}
          </ul>
        )}
      </section>

      <GovernancePoliciesSection wsId={wsId} editable={editable} squads={live} />
    </div>
  );
}

/** One squad's level, its last reason, and (for the boss) a change with a reason. */
function SquadAutonomyRow({ wsId, squad, editable }: { wsId: string; squad: Squad; editable: boolean }) {
  const { t } = useT("governance");
  const { data: view } = useQuery(squadAutonomyOptions(wsId, squad.id));
  const level = view?.level ?? squad.autonomy_level ?? DEFAULT_AUTONOMY_LEVEL;
  const reason = view?.reason ?? "";
  const changedAt = view?.changed_at ?? null;
  const change = useSetSquadAutonomy(wsId, squad.id);

  const [draftLevel, setDraftLevel] = useState<number>(level);
  const [reasonText, setReasonText] = useState("");
  useEffect(() => setDraftLevel(level), [level]);

  const levelKey = AUTONOMY_LEVEL_KEYS[level as AutonomyLevel];
  const canSave = editable && !change.isPending && draftLevel !== level && reasonText.trim() !== "";
  return (
    <li className="flex flex-col gap-3 px-4 py-3">
      <div className="flex min-w-0 flex-wrap items-baseline gap-2">
        <span className="min-w-0 truncate text-body font-medium">{squad.name}</span>
        <span className="text-caption text-muted-foreground">
          {t(($) => $.autonomy.level_tag, { level })}
          {levelKey ? ` ${t(($) => $.autonomy.levels[levelKey].name)}` : ""}
        </span>
      </div>
      {reason ? (
        <p className="text-caption text-muted-foreground">
          {reason}
          {changedAt ? ` · ${new Date(changedAt).toLocaleDateString()}` : ""}
        </p>
      ) : (
        <p className="text-caption text-muted-foreground">{t(($) => $.autonomy.default_level)}</p>
      )}
      {editable ? (
        <form
          className="flex flex-col gap-2 sm:flex-row sm:items-end"
          onSubmit={(event) => {
            event.preventDefault();
            if (!canSave) return;
            change.mutate(
              { level: draftLevel as AutonomyLevel, reason: reasonText.trim() },
              {
                onSuccess: () => {
                  setReasonText("");
                  toast.success(t(($) => $.autonomy.level_saved));
                },
                onError: () => toast.error(t(($) => $.autonomy.level_save_failed)),
              },
            );
          }}
        >
          <div className="flex flex-col gap-1">
            <Label className="text-caption">{t(($) => $.autonomy.level_label)}</Label>
            <Select
              items={LEVELS.map((value) => ({ value: String(value), label: `L${value}` }))}
              value={String(draftLevel)}
              disabled={change.isPending}
              onValueChange={(value) => {
                if (value) setDraftLevel(Number(value));
              }}
            >
              <SelectTrigger aria-label={t(($) => $.autonomy.level_label)} className="w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {LEVELS.map((value) => (
                  <SelectItem key={value} value={String(value)}>
                    {`L${value} ${t(($) => $.autonomy.levels[AUTONOMY_LEVEL_KEYS[value]].name)}`}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <Label htmlFor={`autonomy-reason-${squad.id}`} className="text-caption">
              {t(($) => $.autonomy.reason_label)}
            </Label>
            <Textarea
              id={`autonomy-reason-${squad.id}`}
              value={reasonText}
              maxLength={500}
              rows={2}
              placeholder={t(($) => $.autonomy.reason_placeholder)}
              disabled={change.isPending}
              onChange={(event) => setReasonText(event.target.value)}
            />
          </div>
          <Button type="submit" size="sm" disabled={!canSave}>
            {t(($) => $.autonomy.save_level)}
          </Button>
        </form>
      ) : null}
    </li>
  );
}
