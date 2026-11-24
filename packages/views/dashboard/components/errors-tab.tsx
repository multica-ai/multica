"use client";

import { useMemo, useState } from "react";
import { BarChart3 } from "@multica/ui/icons";
import { NumberFlow } from "@multica/ui/components/ui/number-flow";
import { FAILURE_CLASSES, type FailureClass } from "@multica/core/dashboard";
import { useWorkspacePaths } from "@multica/core/paths";
import { KpiCard } from "../../runtimes/components/shared";
import {
  DailyErrorsChart,
  WeeklyErrorsChart,
  FAILURE_CLASS_COLOR,
  formatRate,
} from "../../runtimes/components/charts";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { CollectionPageState } from "../../layout/collection-page";
import {
  aggregateDailyErrors,
  aggregateWeeklyErrors,
  DELETED_SQUADS_ROW_ID,
  hasRateSample,
  MIN_RATE_SAMPLE,
  OFFENDER_METRIC,
  sortAgentFailures,
  sortSquadFailures,
  type AgentFailureRow,
  type FailureClassRow,
  type FailureReasonRow,
  type FailureTotals,
  type OffenderSort,
  type SquadFailureRow,
} from "../utils";
import { type Dim } from "./dashboard-shared";
import { Segmented } from "./dashboard-shared";
import { DimSegmented } from "./dim-segmented";

// How many offenders the list shows before collapsing the tail behind a
// toggle. The list is ranked by absolute failure count, so the tail is
// agents that failed once or twice — real, but not what anyone opens this
// view to see.
const TOP_OFFENDER_LIMIT = 8;

// Shared by the offender column header and every offender row, so the two
// cannot drift out of alignment.
const OFFENDER_GRID =
  "grid grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_4rem_4rem_4rem] items-center gap-3";

// Translated label for a failure class. The mapping is a switch rather than
// an index into a lookup object so the type checker flags a class added to
// FAILURE_CLASSES without matching copy.
function useFailureClassLabel(): (c: FailureClass) => string {
  const { t } = useT("usage");
  return (c) => {
    switch (c) {
      case "auth":
        return t(($) => $.errors.class.auth);
      case "rate_limit":
        return t(($) => $.errors.class.rate_limit);
      case "timeout":
        return t(($) => $.errors.class.timeout);
      case "provider":
        return t(($) => $.errors.class.provider);
      case "runtime":
        return t(($) => $.errors.class.runtime);
      case "agent":
        return t(($) => $.errors.class.agent);
      case "other":
        return t(($) => $.errors.class.other);
    }
  };
}

/**
 * "What broke", as a full view rather than one compressed card at the bottom
 * of the spend page.
 *
 * Reads top-down the way the question is actually asked: how bad is it (KPIs),
 * when did it happen (trend), what kind of thing broke (mix), and who it broke
 * for (offenders). Each of those used to be a subsection of a single card, or
 * a fifth option on a toggle whose other four were spend metrics.
 */
export function ErrorsTab({
  days,
  allowedDims,
  totals,
  classRows,
  reasonRows,
  agentRows,
  squadRows = [],
  dailyErrors,
  weeklyErrors,
  agents,
  squads = [],
  allowSquadGrouping = false,
  locales,
}: {
  days: number;
  allowedDims: readonly Dim[];
  totals: FailureTotals;
  classRows: FailureClassRow[];
  reasonRows: FailureReasonRow[];
  agentRows: AgentFailureRow[];
  squadRows?: SquadFailureRow[];
  dailyErrors: ReturnType<typeof aggregateDailyErrors>;
  weeklyErrors: ReturnType<typeof aggregateWeeklyErrors>;
  agents: { id: string; name: string }[];
  squads?: { id: string; name: string }[];
  allowSquadGrouping?: boolean;
  locales?: Intl.LocalesArgument;
}) {
  const { t } = useT("usage");
  const classLabel = useFailureClassLabel();

  // Agents affected counts the rows of the offender list below, so the tile and
  // the list can never disagree. Agents this viewer cannot resolve are already
  // folded into one anonymous row upstream, which makes this a lower bound when
  // several hidden agents fail — the alternative, counting them individually,
  // would leak how many private agents exist.
  const affectedAgents = agentRows.length;
  const worst = agentRows[0];
  const worstName = worst
    ? (agents.find((a) => a.id === worst.agentId)?.name ??
      t(($) => $.errors.other_agents))
    : null;

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-1 divide-y rounded-lg border bg-card sm:grid-cols-3 sm:divide-x sm:divide-y-0">
        <KpiCard
          label={t(($) => $.errors.kpi_failed_label, { days })}
          value={
            <NumberFlow
              value={totals.failed}
              locales={locales}
              format={{ maximumFractionDigits: 0 }}
              aria-label={String(totals.failed)}
            />
          }
          hint={t(($) => $.errors.kpi_failed_hint, { total: totals.total })}
        />
        {/* The rate carries its denominator right below it. The Tasks tile on
            the Usage tab quotes a different, smaller failure count — it only
            counts tasks that actually started — and two bare percentages from
            two denominators read as a contradiction unless each says what it
            is counting. */}
        <KpiCard
          label={t(($) => $.errors.kpi_rate_label, { days })}
          value={formatRate(totals.failed, totals.total)}
          hint={t(($) => $.errors.summary, {
            failed: totals.failed,
            total: totals.total,
            rate: formatRate(totals.failed, totals.total),
          })}
        />
        <KpiCard
          label={t(($) => $.errors.kpi_agents_label, { days })}
          value={
            <NumberFlow
              value={affectedAgents}
              locales={locales}
              format={{ maximumFractionDigits: 0 }}
              aria-label={String(affectedAgents)}
            />
          }
          hint={
            worst && worstName
              ? t(($) => $.errors.kpi_agents_hint, {
                  name: worstName,
                  count: worst.failed,
                })
              : undefined
          }
        />
      </div>

      {totals.failed === 0 ? (
        <CollectionPageState icon={BarChart3} title={t(($) => $.errors.no_data)} />
      ) : (
        <>
          <ErrorTrendCard
            allowedDims={allowedDims}
            dailyErrors={dailyErrors}
            weeklyErrors={weeklyErrors}
          />
          <FailureMixCard
            totals={totals}
            classRows={classRows}
            reasonRows={reasonRows}
            classLabel={classLabel}
          />
          <OffendersCard
            agentRows={agentRows}
            squadRows={squadRows}
            agents={agents}
            squads={squads}
            allowSquadGrouping={allowSquadGrouping}
            classLabel={classLabel}
          />
        </>
      )}
    </div>
  );
}

/** Failures over time, on the same x-axis the spend charts use. Promoted from
 *  the fifth option of the spend toggle, where seeing failures meant hiding
 *  spend. */
function ErrorTrendCard({
  allowedDims,
  dailyErrors,
  weeklyErrors,
}: {
  allowedDims: readonly Dim[];
  dailyErrors: ReturnType<typeof aggregateDailyErrors>;
  weeklyErrors: ReturnType<typeof aggregateWeeklyErrors>;
}) {
  const { t } = useT("usage");
  const [dim, setDim] = useState<Dim>("daily");
  const effectiveDim: Dim = allowedDims.includes(dim) ? dim : allowedDims[0]!;
  const weekly = effectiveDim === "weekly";

  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <h4 className="text-body font-semibold">
          {weekly ? t(($) => $.weekly.title_errors) : t(($) => $.daily.title_errors)}
        </h4>
        <DimSegmented allowedDims={allowedDims} value={effectiveDim} onChange={setDim} />
      </div>
      <div className="min-h-[240px]">
        {weekly ? (
          <WeeklyErrorsChart data={weeklyErrors} />
        ) : (
          <DailyErrorsChart data={dailyErrors} />
        )}
      </div>
    </div>
  );
}

/** What kind of thing broke: the class mix, with the raw wire enums one click
 *  away. */
function FailureMixCard({
  totals,
  classRows,
  reasonRows,
  classLabel,
}: {
  totals: FailureTotals;
  classRows: FailureClassRow[];
  reasonRows: FailureReasonRow[];
  classLabel: (c: FailureClass) => string;
}) {
  const { t } = useT("usage");
  const [showReasons, setShowReasons] = useState(false);

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pt-4 pb-3">
        {/* Spells out its own denominator: the rate tile above quotes a rate
            over every run, this section splits the failures alone. */}
        <h4 className="text-body font-semibold">
          {t(($) => $.errors.mix_title, { failed: totals.failed })}
        </h4>
        <button
          type="button"
          onClick={() => setShowReasons((v) => !v)}
          className="text-caption text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
        >
          {showReasons
            ? t(($) => $.errors.hide_reasons)
            : t(($) => $.errors.show_reasons)}
        </button>
      </div>
      <div className="p-4">
        {showReasons ? (
          <ReasonList rows={reasonRows} />
        ) : (
          <ClassComposition rows={classRows} classLabel={classLabel} />
        )}
      </div>
    </div>
  );
}

/** Who it broke for. */
function OffendersCard({
  agentRows,
  squadRows,
  agents,
  squads,
  allowSquadGrouping,
  classLabel,
}: {
  agentRows: AgentFailureRow[];
  squadRows: SquadFailureRow[];
  agents: { id: string; name: string }[];
  squads: { id: string; name: string }[];
  allowSquadGrouping: boolean;
  classLabel: (c: FailureClass) => string;
}) {
  const { t } = useT("usage");
  const wsPaths = useWorkspacePaths();
  const [showAllAgents, setShowAllAgents] = useState(false);
  const [sortBy, setSortBy] = useState<OffenderSort>("failed");
  const [groupBy, setGroupBy] = useState<"agent" | "squad">("agent");

  const effectiveGroupBy = allowSquadGrouping ? groupBy : "agent";

  const sortOptions = useMemo(
    () => [
      { value: "failed" as const, label: t(($) => $.errors.sort_failed) },
      { value: "rate" as const, label: t(($) => $.errors.sort_rate) },
    ],
    [t],
  );

  const groupOptions = useMemo(
    () => [
      { value: "agent" as const, label: t(($) => $.leaderboard.group_agent) },
      { value: "squad" as const, label: t(($) => $.leaderboard.group_squad) },
    ],
    [t],
  );

  const sortedAgents = useMemo(
    () => sortAgentFailures(agentRows, sortBy),
    [agentRows, sortBy],
  );

  const sortedSquads = useMemo(
    () => sortSquadFailures(squadRows, sortBy),
    [squadRows, sortBy],
  );

  const activeCount =
    effectiveGroupBy === "squad" ? sortedSquads.length : sortedAgents.length;

  const leader =
    effectiveGroupBy === "squad" ? sortedSquads[0] : sortedAgents[0];
  const maxValue = leader
    ? sortBy === "failed"
      ? leader.failed
      : leader.rate
    : 0;

  const visibleAgents = showAllAgents
    ? sortedAgents
    : sortedAgents.slice(0, TOP_OFFENDER_LIMIT);

  const visibleSquads = showAllAgents
    ? sortedSquads
    : sortedSquads.slice(0, TOP_OFFENDER_LIMIT);

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pt-4 pb-3">
        <div className="flex flex-wrap items-center gap-2.5">
          <h4 className="text-body font-semibold">
            {effectiveGroupBy === "squad"
              ? t(($) => $.errors.by_squad)
              : t(($) => $.errors.by_agent)}
          </h4>
          {allowSquadGrouping ? (
            <Segmented
              label={t(($) => $.leaderboard.group_label)}
              value={effectiveGroupBy}
              onChange={(next) => {
                setShowAllAgents(false);
                setGroupBy(next);
              }}
              options={groupOptions}
            />
          ) : null}
        </div>
        <div className="flex flex-wrap items-center justify-end gap-3">
          <Segmented
            label={t(($) => $.errors.sort_label)}
            value={sortBy}
            onChange={setSortBy}
            options={sortOptions}
          />
          {activeCount > TOP_OFFENDER_LIMIT ? (
            <button
              type="button"
              onClick={() => setShowAllAgents((v) => !v)}
              className="text-caption text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
            >
              {showAllAgents
                ? t(($) => $.errors.show_less, { count: TOP_OFFENDER_LIMIT })
                : t(($) => $.errors.show_all, { count: activeCount })}
            </button>
          ) : null}
        </div>
      </div>
      <div className="p-4 pt-0">
        {activeCount > 0 ? (
          <div
            className={`${OFFENDER_GRID} border-b py-2 text-caption font-medium text-muted-foreground`}
          >
            <span>
              {effectiveGroupBy === "squad"
                ? t(($) => $.leaderboard.header_squad)
                : t(($) => $.errors.header_agent)}
            </span>
            <span />
            <span
              className={`text-right ${sortBy === "failed" ? "text-foreground" : ""}`}
            >
              {t(($) => $.errors.header_failed)}
            </span>
            <span className="text-right">{t(($) => $.errors.header_runs)}</span>
            <span
              className={`text-right ${sortBy === "rate" ? "text-foreground" : ""}`}
            >
              {t(($) => $.errors.header_rate)}
            </span>
          </div>
        ) : null}
        <ul
          aria-label={
            effectiveGroupBy === "squad"
              ? t(($) => $.errors.by_squad)
              : t(($) => $.errors.by_agent)
          }
          className="divide-y"
        >
          {effectiveGroupBy === "squad"
            ? visibleSquads.map((row) => {
                const isDeleted = row.squadId === DELETED_SQUADS_ROW_ID;
                const squad = squads.find((s) => s.id === row.squadId);
                const labelText = isDeleted
                  ? t(($) => $.leaderboard.deleted_squads)
                  : squad?.name ?? null;
                return (
                  <AgentFailureItem
                    key={row.squadId}
                    row={{
                      agentId: row.squadId,
                      failed: row.failed,
                      total: row.total,
                      rate: row.rate,
                      classes: row.classes,
                    }}
                    name={labelText}
                    href={squad ? wsPaths.squadDetail(squad.id) : null}
                    italic={isDeleted || !labelText}
                    maxValue={maxValue}
                    sortBy={sortBy}
                    classLabel={classLabel}
                  />
                );
              })
            : visibleAgents.map((row) => (
                <AgentFailureItem
                  key={row.agentId}
                  row={row}
                  name={agents.find((a) => a.id === row.agentId)?.name ?? null}
                  maxValue={maxValue}
                  sortBy={sortBy}
                  classLabel={classLabel}
                />
              ))}
        </ul>
      </div>
    </div>
  );
}

/**
 * Class breakdown as one 100%-stacked bar plus a legend.
 *
 * The question this answers is "what is the mix", and a single bar shows
 * share-of-total directly — with separate bars the reader has to compare
 * lengths and mentally total them.
 */
function ClassComposition({
  rows,
  classLabel,
}: {
  rows: FailureClassRow[];
  classLabel: (c: FailureClass) => string;
}) {
  const { t } = useT("usage");
  const total = rows.reduce((sum, r) => sum + r.count, 0);
  if (total === 0) return null;

  return (
    <div className="space-y-2.5">
      {/* Segments are ordered by count desc (the aggregator's order), so the
          bar reads heaviest-first left to right. */}
      <div className="flex h-2 w-full overflow-hidden rounded-full bg-muted">
        {rows.map((row) => (
          <div
            key={row.failureClass}
            className="h-full transition-[width] duration-300 ease-out"
            style={{
              width: `${(row.count / total) * 100}%`,
              backgroundColor: FAILURE_CLASS_COLOR[row.failureClass],
            }}
          />
        ))}
      </div>
      <ul
        aria-label={t(($) => $.errors.mix_label)}
        className="flex flex-wrap items-center gap-x-4 gap-y-1.5"
      >
        {rows.map((row) => (
          <li key={row.failureClass} className="flex items-center gap-1.5">
            <span
              aria-hidden
              className="h-2 w-2 shrink-0 rounded-[2px]"
              style={{ backgroundColor: FAILURE_CLASS_COLOR[row.failureClass] }}
            />
            <span className="text-caption">{classLabel(row.failureClass)}</span>
            <span className="text-caption tabular-nums text-muted-foreground">
              {row.count}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

/**
 * Raw `failure_reason` values behind the class summary. Unlocalised on
 * purpose: they are the backend's wire enum, and an operator pasting one into
 * a log search or an issue needs the exact string.
 */
function ReasonList({ rows }: { rows: FailureReasonRow[] }) {
  const { t } = useT("usage");
  return (
    <ul
      aria-label={t(($) => $.errors.codes_label)}
      className="grid grid-cols-1 gap-x-6 gap-y-1.5 sm:grid-cols-2"
    >
      {rows.map((row) => (
        <li key={row.reason} className="flex items-center justify-between gap-2">
          <span className="flex min-w-0 items-center gap-2">
            <span
              aria-hidden
              className="h-2 w-2 shrink-0 rounded-[2px]"
              style={{ backgroundColor: FAILURE_CLASS_COLOR[row.failureClass] }}
            />
            <code className="truncate text-caption text-muted-foreground">
              {row.reason}
            </code>
          </span>
          <span className="shrink-0 text-caption tabular-nums">{row.count}</span>
        </li>
      ))}
    </ul>
  );
}

/**
 * One offender row, shaped like the leaderboard row on the Usage tab:
 * identity, then a proportional bar, then one column per number.
 *
 * The bar measures whatever the list is currently sorted by, and the matching
 * column is emphasised — the same lockstep the leaderboard keeps.
 *
 * The bar is stacked by failure class, which is also the only thing its colour
 * means. A row that fails one way is a solid block; a row failing five ways is
 * visibly striped — a distinction a single dominant-class badge erased.
 */
function AgentFailureItem({
  row,
  name,
  href,
  italic,
  maxValue,
  sortBy,
  classLabel,
}: {
  row: AgentFailureRow;
  name: string | null;
  href?: string | null;
  italic?: boolean;
  maxValue: number;
  sortBy: OffenderSort;
  classLabel: (c: FailureClass) => string;
}) {
  const { t } = useT("usage");
  const wsPaths = useWorkspacePaths();

  const segments = FAILURE_CLASSES.filter((c) => row.classes[c] > 0);
  const composition = segments
    .map((c) => `${classLabel(c)} ${row.classes[c]}`)
    .join(" · ");

  const value = OFFENDER_METRIC[sortBy](row);
  const pct = maxValue > 0 ? Math.min(100, (value / maxValue) * 100) : 0;

  const weakSample = !hasRateSample(row);
  const resolvedHref =
    href !== undefined
      ? href
      : name
        ? `${wsPaths.agentDetail(row.agentId)}?view=overview`
        : null;

  const label = (
    <span
      className={`block truncate text-caption${italic || !name ? " italic text-muted-foreground" : ""}`}
    >
      {name ?? t(($) => $.errors.other_agents)}
    </span>
  );

  return (
    <li className={`${OFFENDER_GRID} py-2`}>
      {resolvedHref && name ? (
        <AppLink
          href={resolvedHref}
          newTabTitle={name}
          className="min-w-0 hover:underline"
        >
          {label}
        </AppLink>
      ) : (
        label
      )}
      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
        <div
          role="img"
          aria-label={composition}
          title={composition}
          className="flex h-full overflow-hidden rounded-full transition-[width] duration-300 ease-out"
          style={{ width: `${pct}%` }}
        >
          {segments.map((c) => (
            <div
              key={c}
              className="h-full"
              style={{
                width: `${(row.classes[c] / row.failed) * 100}%`,
                backgroundColor: FAILURE_CLASS_COLOR[c],
              }}
            />
          ))}
        </div>
      </div>
      <span
        className={`text-right text-caption tabular-nums ${sortBy === "failed" ? "font-medium text-foreground" : "text-muted-foreground"}`}
      >
        {row.failed}
      </span>
      <span className="text-right text-caption tabular-nums text-muted-foreground">
        {row.total}
      </span>
      <span
        title={
          weakSample
            ? t(($) => $.errors.low_sample, { count: MIN_RATE_SAMPLE })
            : undefined
        }
        className={`text-right text-caption tabular-nums ${
          sortBy === "rate" && !weakSample
            ? "font-medium text-foreground"
            : "text-muted-foreground"
        }`}
      >
        {formatRate(row.failed, row.total)}
      </span>
    </li>
  );
}
