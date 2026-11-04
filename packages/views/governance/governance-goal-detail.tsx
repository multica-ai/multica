"use client";

import { useState } from "react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import {
  canGovern,
  goalDetailOptions,
  metricReadingsOptions,
  useActivateGoal,
  useSetGoalConfidence,
  useSetGoalStatus,
  type GoalConfidence,
  type KeyResult,
} from "@multica/core/governance";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Progress } from "@multica/ui/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { useT } from "../i18n";
import { GoalConfidenceChip, GoalStatusBadge } from "./goal-badges";
import { GovernanceAddReading } from "./governance-add-reading";
import { GovernanceSparkline } from "./governance-sparkline";
import { progressPercent } from "./goals-tree";

const OVERRIDES: GoalConfidence[] = ["on_track", "at_risk", "off_track"];
const CLOSE_AS = ["achieved", "missed", "dropped"] as const;
const CLEAR = "__clear__";

/** One goal: its readings per key result, the links it has, and the boss's actions. */
export function GovernanceGoalDetail({ goalId, onClose }: { goalId: string; onClose: () => void }) {
  const { t } = useT("governance");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const detail = useQuery(goalDetailOptions(wsId, goalId));
  const activate = useActivateGoal(wsId);
  const setStatus = useSetGoalStatus(wsId);
  const setConfidence = useSetGoalConfidence(wsId);
  const [overrideValue, setOverrideValue] = useState<string>(CLEAR);
  const [overrideNote, setOverrideNote] = useState("");

  const goal = detail.data?.goal;
  if (detail.isPending) return <p className="text-muted-foreground">{t(($) => $.goals.loading)}</p>;
  if (!goal || !goal.id) return <p className="text-destructive">{t(($) => $.goals.detail.load_failed)}</p>;

  const editable = canGovern(role);
  const fail = () => toast.error(t(($) => $.goals.detail.action_failed));
  const percent = progressPercent(goal.progress);
  // Readings for a new reading go where the first key result reads; the goal's own scope otherwise.
  const firstKR = goal.key_results[0];
  const readingScope = firstKR
    ? { type: firstKR.scope_type, id: firstKR.scope_id }
    : { type: goal.scope_type, id: goal.scope_id };

  const saveOverride = () => {
    const confidence = overrideValue === CLEAR ? null : (overrideValue as GoalConfidence);
    setConfidence.mutate(
      { id: goal.id, confidence, note: overrideNote },
      {
        onSuccess: () => {
          setOverrideNote("");
          toast.success(t(($) => $.goals.detail.override_saved));
        },
        onError: fail,
      },
    );
  };

  return (
    <section aria-label={goal.title} className="flex flex-col gap-5 rounded-lg border bg-card p-4">
      <header className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-body font-semibold">{goal.title}</h2>
          <Button type="button" variant="ghost" size="sm" onClick={onClose}>
            {t(($) => $.goals.detail.close)}
          </Button>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <GoalStatusBadge status={goal.status} />
          <GoalConfidenceChip confidence={goal.confidence} overridden={!!goal.confidence_override} />
          <span className="text-caption text-muted-foreground">
            {t(($) => $.goals.detail.period, { start: goal.period_start, end: goal.period_end })}
          </span>
        </div>
        {goal.confidence_override ? (
          <p className="text-caption text-muted-foreground" data-testid="goal-override-note">
            {t(($) => $.goals.detail.override_note, {
              derived: t(($) => $.goals.confidence[goal.derived_confidence]),
              note: goal.confidence_note,
            })}
          </p>
        ) : null}
        {goal.why ? <p className="text-muted-foreground">{goal.why}</p> : null}
        <div className="flex items-center gap-3">
          {percent == null ? (
            <span className="text-caption text-muted-foreground">{t(($) => $.goals.progress_none)}</span>
          ) : (
            <Progress value={percent} className="w-full max-w-sm" />
          )}
          {percent == null ? null : <span className="text-caption tabular-nums">{t(($) => $.goals.progress, { percent })}</span>}
        </div>
      </header>

      <div className="overflow-x-auto">
        <table className="w-full text-left text-caption">
          <caption className="sr-only">{t(($) => $.goals.detail.key_results)}</caption>
          <thead className="text-muted-foreground">
            <tr>
              <th className="py-1 pr-3 font-medium">{t(($) => $.goals.detail.kr_title)}</th>
              <th className="py-1 pr-3 font-medium">{t(($) => $.goals.detail.kr_range)}</th>
              <th className="py-1 pr-3 font-medium">{t(($) => $.goals.detail.kr_now)}</th>
              <th className="py-1 pr-3 font-medium">{t(($) => $.goals.detail.kr_progress)}</th>
              <th className="py-1 font-medium">{t(($) => $.goals.detail.kr_trend)}</th>
            </tr>
          </thead>
          <tbody>
            {goal.key_results.map((kr) => (
              <KeyResultRow key={kr.id} kr={kr} wsId={wsId} />
            ))}
          </tbody>
        </table>
      </div>

      {detail.data?.child_goals.length ? (
        <div className="flex flex-col gap-1">
          <h3 className="text-caption font-medium">{t(($) => $.goals.detail.child_goals)}</h3>
          <ul className="flex flex-col gap-1 text-caption">
            {detail.data.child_goals.map((child) => (
              <li key={child.id}>
                {child.title} · {t(($) => $.goals.status[child.status])}
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {goal.source_proposal_id ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.goals.detail.source_proposal, { id: goal.source_proposal_id })}
        </p>
      ) : null}

      {editable ? (
        <div className="flex flex-col gap-4 border-t pt-4">
          {goal.status === "draft" ? (
            <div>
              <Button
                type="button"
                size="sm"
                disabled={activate.isPending}
                onClick={() => activate.mutate(goal.id, { onError: fail })}
              >
                {t(($) => $.goals.detail.activate)}
              </Button>
            </div>
          ) : null}
          {goal.status === "active" ? (
            <>
              <div className="flex flex-wrap gap-2">
                {CLOSE_AS.map((status) => (
                  <Button
                    key={status}
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={setStatus.isPending}
                    onClick={() => setStatus.mutate({ id: goal.id, status }, { onError: fail })}
                  >
                    {t(($) => $.goals.detail.close_as[status])}
                  </Button>
                ))}
              </div>
              <div className="flex flex-col gap-2">
                <h3 className="text-caption font-medium">{t(($) => $.goals.detail.override_heading)}</h3>
                <div className="flex flex-wrap items-end gap-2">
                  <Select
                    value={overrideValue}
                    onValueChange={(next) => setOverrideValue(next ?? CLEAR)}
                    items={[
                      { value: CLEAR, label: t(($) => $.goals.detail.override_clear) },
                      ...OVERRIDES.map((c) => ({ value: c, label: t(($) => $.goals.confidence[c]) })),
                    ]}
                  >
                    <SelectTrigger aria-label={t(($) => $.goals.detail.override_heading)} className="w-44">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={CLEAR}>{t(($) => $.goals.detail.override_clear)}</SelectItem>
                      {OVERRIDES.map((c) => (
                        <SelectItem key={c} value={c}>
                          {t(($) => $.goals.confidence[c])}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Input
                    aria-label={t(($) => $.goals.detail.override_why)}
                    placeholder={t(($) => $.goals.detail.override_why)}
                    value={overrideNote}
                    onChange={(e) => setOverrideNote(e.target.value)}
                    maxLength={2000}
                    className="max-w-md"
                  />
                  <Button
                    type="button"
                    size="sm"
                    disabled={setConfidence.isPending || (overrideValue !== CLEAR && !overrideNote.trim())}
                    onClick={saveOverride}
                  >
                    {t(($) => $.goals.detail.override_save)}
                  </Button>
                </div>
              </div>
            </>
          ) : null}
        </div>
      ) : null}

      <div className="flex flex-col gap-2 border-t pt-4">
        <h3 className="text-caption font-medium">{t(($) => $.reading.title)}</h3>
        <GovernanceAddReading wsId={wsId} defaultScope={readingScope} defaultKey={firstKR?.metric_key} />
      </div>
    </section>
  );
}

/** One key result: its range, the latest reading and its progress, with the readings as a line. */
function KeyResultRow({ kr, wsId }: { kr: KeyResult; wsId: string }) {
  const { t } = useT("governance");
  const percent = progressPercent(kr.progress);
  const series = useQuery(metricReadingsOptions(wsId, kr.metric_key, { scope_type: kr.scope_type, scope_id: kr.scope_id, limit: 60 }));
  // The list is newest first; the line reads oldest first.
  const values = (series.data ?? []).map((r) => r.value).reverse();
  return (
    <tr className="border-t">
      <td className="py-2 pr-3">
        {kr.title}
        <div className="text-muted-foreground">{kr.metric_key}</div>
      </td>
      <td className="py-2 pr-3 tabular-nums">{t(($) => $.goals.detail.kr_from_to, { start: kr.start_value, target: kr.target_value })}</td>
      <td className="py-2 pr-3 tabular-nums">
        {kr.current_value == null ? t(($) => $.goals.no_reading) : String(kr.current_value)}
      </td>
      <td className="py-2 pr-3 tabular-nums">{percent == null ? "—" : `${percent}%`}</td>
      <td className="py-2">
        <GovernanceSparkline values={values} label={t(($) => $.goals.detail.kr_trend_label, { title: kr.title })} />
      </td>
    </tr>
  );
}
