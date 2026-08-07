"use client";

import { useMemo, useState } from "react";
import { BarChart3 } from "lucide-react";
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
import {
  aggregateDailyErrors,
  aggregateWeeklyErrors,
  hasRateSample,
  MIN_RATE_SAMPLE,
  OFFENDER_METRIC,
  sortAgentFailures,
  type AgentFailureRow,
  type FailureClassRow,
  type FailureReasonRow,
  type FailureTotals,
  type OffenderSort,
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
  dailyErrors,
  weeklyErrors,
  agents,
  locales,
}: {
  days: number;
  allowedDims: readonly Dim[];
  totals: FailureTotals;
  classRows: FailureClassRow[];
  reasonRows: FailureReasonRow[];
  agentRows: AgentFailureRow[];
  dailyErrors: ReturnType<typeof aggregateDailyErrors>;
  weeklyErrors: ReturnType<typeof aggregateWeeklyErrors>;
  agents: { id: string; name: string }[];
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
        <div className="flex flex-col items-center rounded-lg border border-dashed py-12 text-center">
          <BarChart3 className="h-6 w-6 text-faint-foreground" />
          <p className="mt-3 text-caption text-muted-foreground">
            {t(($) => $.errors.no_data)}
          </p>
        </div>
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
            agents={agents}
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
  agents,
  classLabel,
}: {
  agentRows: AgentFailureRow[];
  agents: { id: string; name: string }[];
  classLabel: (c: FailureClass) => string;
}) {
  const { t } = useT("usage");
  const [showAllAgents, setShowAllAgents] = useState(false);
  const [sortBy, setSortBy] = useState<OffenderSort>("failed");

  const sortOptions = useMemo(
    () => [
      { value: "failed" as const, label: t(($) => $.errors.sort_failed) },
      { value: "rate" as const, label: t(($) => $.errors.sort_rate) },
    ],
    [t],
  );

  const sortedAgents = useMemo(
    () => sortAgentFailures(agentRows, sortBy),
    [agentRows, sortBy],
  );

  // The leader fills the track, measured over every row rather than the
  // visible ones so a bar means the same thing collapsed and expanded. Reading
  // it off the leader (instead of a max over the raw rows) also keeps the Rate
  // scale usable: a demoted small-sample row can out-rate the leader, and
  // scaling to it would squash every meaningful bar to a sliver.
  const leader = sortedAgents[0];
  const maxValue = leader ? OFFENDER_METRIC[sortBy](leader) : 0;

  const visibleAgents = showAllAgents
    ? sortedAgents
    : sortedAgents.slice(0, TOP_OFFENDER_LIMIT);

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pt-4 pb-3">
        <h4 className="text-body font-semibold">{t(($) => $.errors.by_agent)}</h4>
        <div className="flex flex-wrap items-center justify-end gap-3">
          <Segmented
            label={t(($) => $.errors.sort_label)}
            value={sortBy}
            onChange={setSortBy}
            options={sortOptions}
          />
          {sortedAgents.length > TOP_OFFENDER_LIMIT ? (
            <button
              type="button"
              onClick={() => setShowAllAgents((v) => !v)}
              className="text-caption text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
            >
              {showAllAgents
                ? t(($) => $.errors.show_less, { count: TOP_OFFENDER_LIMIT })
                : t(($) => $.errors.show_all, { count: sortedAgents.length })}
            </button>
          ) : null}
        </div>
      </div>
      <div className="p-4 pt-0">
        {/* Column headers, as on the leaderboard: `4 / 10 · 40%` was one
            unlabelled blob and the reader had to guess which number was
            which. The active metric's column is emphasised so it is
            obvious what the ranking and the bars measure. */}
        {sortedAgents.length > 0 ? (
          <div
            className={`${OFFENDER_GRID} border-b py-2 text-caption font-medium text-muted-foreground`}
          >
            <span>{t(($) => $.errors.header_agent)}</span>
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
        <ul aria-label={t(($) => $.errors.by_agent)} className="divide-y">
          {visibleAgents.map((row) => (
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
  maxValue,
  sortBy,
  classLabel,
}: {
  row: AgentFailureRow;
  name: string | null;
  maxValue: number;
  sortBy: OffenderSort;
  classLabel: (c: FailureClass) => string;
}) {
  const { t } = useT("usage");
  const wsPaths = useWorkspacePaths();

  const segments = FAILURE_CLASSES.filter((c) => row.classes[c] > 0);
  // Text equivalent of the stacked bar. The bar is the only place the class
  // split is rendered, so it has to carry a name for screen readers as well as
  // a hover affordance for everyone else.
  const composition = segments
    .map((c) => `${classLabel(c)} ${row.classes[c]}`)
    .join(" · ");

  // Clamped, not just scaled: under the Rate ranking a small-sample row is
  // demoted below the leader while still able to carry a higher rate, and an
  // unclamped width would overflow the track.
  const value = OFFENDER_METRIC[sortBy](row);
  const pct = maxValue > 0 ? Math.min(100, (value / maxValue) * 100) : 0;

  // Below MIN_RATE_SAMPLE runs the rate is arithmetic, not signal (1/1 is
  // 100%). Those rows sort last under Rate and never take the emphasis that
  // marks the active column, but they keep rendering and say why on hover.
  const weakSample = !hasRateSample(row);

  // The row links into the agent's Overview, whose ActivityTab lists recent
  // runs with each failure's reason — the drill-down from "this agent is the
  // problem" to the actual failed runs. NOT the Work tab: that one lists the
  // issues assigned to the agent, which is a different question entirely.
  //
  // An agent with no resolvable name is either hard-deleted or private to
  // someone else; either way there is no page to open and no name to show,
  // so the row degrades to a neutral placeholder. Rendering `row.agentId`
  // here would leak a bare UUID — and, for a private agent, leak its
  // existence and failure profile to a member who cannot see it.
  const label = (
    <span
      className={`block truncate text-caption${name ? "" : " italic text-muted-foreground"}`}
    >
      {name ?? t(($) => $.errors.other_agents)}
    </span>
  );

  return (
    <li className={`${OFFENDER_GRID} py-2`}>
      {name ? (
        <AppLink
          href={`${wsPaths.agentDetail(row.agentId)}?view=overview`}
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
