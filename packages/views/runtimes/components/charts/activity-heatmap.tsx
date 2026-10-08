import { useMemo } from "react";
import type { RuntimeUsage } from "@multica/core/types";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import { addDaysIso, estimateCost, formatUsd, todayIso, weekStartIso } from "../../utils";
import { useLocale, useT } from "../../../i18n";

// 26 weeks (~6 months) gives the heatmap real presence in the wider chart
// card and turns "long-view" into a meaningful tab — a 13-week strip looked
// cramped. Cells at 16px (vs GitHub's 11) keep the calendar-square density
// readable at this scale.
const HEATMAP_WEEKS = 26;
const CELL_SIZE = 16;
const CELL_GAP = 3;
// Monday-first row order, matching ISO 8601 and the rest of the Weekly
// aggregation (see #MUL-2382). Labelling alternating rows keeps the density
// readable without tying the chart structure to one language.
const LABELED_WEEKDAY_INDICES = new Set([0, 2, 4]);
// 2026-01-05 is a Monday, so walking 7 days from here lines the formatted
// names up with the Monday-first `dayOfWeek` index.
const WEEKDAY_ANCHOR = Date.UTC(2026, 0, 5);

// Cells use the brand-derived chart-1 hue with descending opacity instead
// of a neutral foreground fade, so the heatmap reads as part of the same
// visual family as Daily cost (chart-1 stack) rather than a separate
// monochrome surface. Level 0 stays neutral muted to clearly mean "no
// activity" (not "very faint activity").
function getHeatmapColor(level: number): string {
  if (level === 0) return "var(--color-muted)";
  const opacities = ["20%", "45%", "70%", "100%"];
  return `color-mix(in oklch, var(--color-chart-1) ${opacities[level - 1]}, transparent)`;
}

function fmtDate(iso: string, locale: string): string {
  return new Date(iso + "T00:00:00").toLocaleString(locale, {
    month: "short",
    day: "numeric",
  });
}

// Row labels come from Intl rather than the locale bundle: for every locale
// we ship, its short weekday names are exactly the strings we would hand-
// translate, and the month labels below already resolve the same way. One
// less set of strings to keep in sync when a locale is added.
function fmtWeekdays(locale: string): string[] {
  const fmt = new Intl.DateTimeFormat(locale, {
    weekday: "short",
    timeZone: "UTC",
  });
  return Array.from({ length: 7 }, (_, i) =>
    fmt.format(new Date(WEEKDAY_ANCHOR + i * 86_400_000)),
  );
}

interface Insights {
  busiestDay: { date: string; cost: number } | null;
  busyDayIndex: number | null;
  busyDayAvg: number;
  quietDayIndex: number | null;
  quietDayAvg: number;
  totalCost: number;
  windowDays: number;
}

export function ActivityHeatmap({
  usage,
  tz,
}: {
  usage: RuntimeUsage[];
  tz: string;
}) {
  const { t } = useT("runtimes");
  const locale = useLocale();
  const weekdayLabels = useMemo(() => fmtWeekdays(locale), [locale]);
  // Memo dep — estimateCost (called inside the body below) consults the
  // user-override store, so saving a custom rate must invalidate the cells.
  const pricings = useCustomPricingStore((s) => s.pricings);
  const { cells, monthLabels, insights } = useMemo(() => {
    // Sum priced cost per day. Cost (not tokens) gives the colour scale a
    // financial meaning that lines up with the rest of the page — a "hot"
    // square here means the same thing as a tall bar in Daily cost.
    const dateCost = new Map<string, number>();
    for (const u of usage) {
      dateCost.set(u.date, (dateCost.get(u.date) ?? 0) + estimateCost(u));
    }

    // Anchor the grid on the Monday of the week containing "today" in the
    // viewer's tz, then walk back HEATMAP_WEEKS-1 weeks. All dates are
    // string-based YYYY-MM-DD so the host browser's tz can't shift a column.
    // We stop drawing cells once we pass `today` so the in-progress week is
    // partial (cells for "tomorrow onward" aren't rendered) — matches the
    // Weekly chart's partial-week treatment.
    const today = todayIso(tz);
    const lastWeekStart = weekStartIso(today);
    const startDate = addDaysIso(lastWeekStart, -(HEATMAP_WEEKS - 1) * 7);
    const todayIndex = (HEATMAP_WEEKS - 1) * 7 + ((() => {
      // Monday-based weekday of `today`: 0 = Mon ... 6 = Sun. Computed via
      // string subtraction so the host timezone can't shift the value.
      const [y, m, d] = today.split("-").map(Number);
      const dt = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
      return (dt.getUTCDay() + 6) % 7;
    })());

    const allCells: {
      date: string;
      dayOfWeek: number; // 0 = Mon ... 6 = Sun
      week: number;
      cost: number;
    }[] = [];
    for (let i = 0; i <= todayIndex; i++) {
      const dateStr = addDaysIso(startDate, i);
      const dayOfWeek = i % 7;
      const week = Math.floor(i / 7);
      allCells.push({
        date: dateStr,
        dayOfWeek,
        week,
        cost: dateCost.get(dateStr) ?? 0,
      });
    }

    const nonZero = allCells.filter((c) => c.cost > 0).map((c) => c.cost);
    nonZero.sort((a, b) => a - b);
    const getLevel = (cost: number) => {
      if (cost === 0) return 0;
      if (nonZero.length <= 1) return 4;
      const p = nonZero.indexOf(cost) / (nonZero.length - 1);
      if (p <= 0.25) return 1;
      if (p <= 0.5) return 2;
      if (p <= 0.75) return 3;
      return 4;
    };

    const cellsWithLevel = allCells.map((c) => ({
      ...c,
      level: getLevel(c.cost),
    }));

    const months: { label: string; week: number }[] = [];
    let lastMonth = -1;
    for (const c of cellsWithLevel) {
      const month = new Date(c.date + "T00:00:00").getMonth();
      if (month !== lastMonth && c.dayOfWeek === 0) {
        months.push({
          label: new Date(c.date + "T00:00:00").toLocaleString(locale, {
            month: "short",
          }),
          week: c.week,
        });
        lastMonth = month;
      }
    }

    // Insights derived from the same cells so the colour scale, the busiest
    // square, and the side-panel numbers can never disagree.
    let busiestDay: { date: string; cost: number } | null = null;
    let totalCost = 0;
    const weekdaySum = [0, 0, 0, 0, 0, 0, 0];
    const weekdayCount = [0, 0, 0, 0, 0, 0, 0];
    for (const c of allCells) {
      totalCost += c.cost;
      weekdaySum[c.dayOfWeek] = (weekdaySum[c.dayOfWeek] ?? 0) + c.cost;
      weekdayCount[c.dayOfWeek] = (weekdayCount[c.dayOfWeek] ?? 0) + 1;
      if (c.cost > 0 && (!busiestDay || c.cost > busiestDay.cost)) {
        busiestDay = { date: c.date, cost: c.cost };
      }
    }
    const weekdayAvg = weekdaySum.map((s, i) => {
      const count = weekdayCount[i] ?? 0;
      return count > 0 ? s / count : 0;
    });
    let busyDayIndex: number | null = null;
    let busyDayAvg = 0;
    let quietDayIndex: number | null = null;
    let quietDayAvg = Number.POSITIVE_INFINITY;
    weekdayAvg.forEach((avg, i) => {
      if (avg > busyDayAvg) {
        busyDayAvg = avg;
        busyDayIndex = i;
      }
      if (avg < quietDayAvg) {
        quietDayAvg = avg;
        quietDayIndex = i;
      }
    });
    if (quietDayAvg === Number.POSITIVE_INFINITY) quietDayAvg = 0;
    // When the window has no spend at all, the busy / quiet weekday picks
    // are noise (every weekday averaged to 0). Suppress them.
    if (totalCost === 0) {
      busyDayIndex = null;
      quietDayIndex = null;
    }

    const insights: Insights = {
      busiestDay,
      busyDayIndex,
      busyDayAvg,
      quietDayIndex,
      quietDayAvg,
      totalCost,
      windowDays: allCells.length,
    };

    return { cells: cellsWithLevel, monthLabels: months, insights };
  }, [usage, pricings, tz, locale]);

  const labelWidth = 28;
  const svgWidth = labelWidth + HEATMAP_WEEKS * (CELL_SIZE + CELL_GAP);
  const svgHeight = 14 + 7 * (CELL_SIZE + CELL_GAP);

  // Vertical stack: heatmap centered up top, insights as a 4-cell stat
  // strip below (separated by a hairline). Stacking guarantees the parent
  // card width is decided entirely by its own grid cell — never by the
  // SVG's intrinsic 249px or by the insight labels — and switching to /
  // from this tab no longer changes the card's apparent width.
  return (
    <div className="space-y-4">
      <div className="flex flex-col items-center gap-2">
        <div className="overflow-x-auto">
          <svg width={svgWidth} height={svgHeight} className="block">
            {monthLabels.map((m) => (
              <text
                key={`${m.label}-${m.week}`}
                x={labelWidth + m.week * (CELL_SIZE + CELL_GAP)}
                y={10}
                className="fill-muted-foreground"
                fontSize={9}
              >
                {m.label}
              </text>
            ))}
            {weekdayLabels.map((label, i) =>
              LABELED_WEEKDAY_INDICES.has(i) ? (
                <text
                  key={i}
                  x={0}
                  y={14 + i * (CELL_SIZE + CELL_GAP) + CELL_SIZE - 1}
                  className="fill-muted-foreground"
                  fontSize={9}
                >
                  {label}
                </text>
              ) : null,
            )}
            {cells.map((c) => (
              <rect
                key={c.date}
                x={labelWidth + c.week * (CELL_SIZE + CELL_GAP)}
                y={14 + c.dayOfWeek * (CELL_SIZE + CELL_GAP)}
                width={CELL_SIZE}
                height={CELL_SIZE}
                rx={3}
                fill={getHeatmapColor(c.level)}
                className="transition-colors"
              >
                {/* The tooltip date stays ISO on purpose: it is the exact
                    day key behind the cell, and readers compare it against
                    the API / CLI usage rows, which are ISO too. Only the
                    prose around it is translated. */}
                <title>
                  {c.date}:{" "}
                  {c.cost > 0
                    ? `$${c.cost.toFixed(2)}`
                    : t(($) => $.charts.heatmap_no_activity)}
                </title>
              </rect>
            ))}
          </svg>
        </div>
        <div className="flex items-center gap-1 text-micro text-muted-foreground">
          <span>{t(($) => $.charts.heatmap_less)}</span>
          {[0, 1, 2, 3, 4].map((level) => (
            <div
              key={level}
              className="h-[10px] w-[10px] rounded-[2px]"
              style={{ backgroundColor: getHeatmapColor(level) }}
            />
          ))}
          <span>{t(($) => $.charts.heatmap_more)}</span>
        </div>
      </div>

      <InsightsRow
        insights={insights}
        locale={locale}
        weekdayLabels={weekdayLabels}
      />
    </div>
  );
}

// Horizontal stat strip beneath the heatmap. Mirrors the page-top KPI
// hero pattern (label → big value → sub) but at smaller scale to stay
// secondary. 4 columns on desktop, 2 on narrow screens.
function InsightsRow({
  insights,
  locale,
  weekdayLabels,
}: {
  insights: Insights;
  locale: string;
  weekdayLabels: string[];
}) {
  const { t } = useT("runtimes");
  const {
    busiestDay,
    busyDayIndex,
    busyDayAvg,
    quietDayIndex,
    quietDayAvg,
    totalCost,
    windowDays,
  } = insights;
  return (
    <dl className="grid grid-cols-2 gap-x-4 gap-y-3 border-t pt-3 sm:grid-cols-4">
      <Insight
        label={t(($) => $.charts.heatmap_busiest_day)}
        value={busiestDay ? fmtDate(busiestDay.date, locale) : "—"}
        sub={busiestDay ? formatUsd(busiestDay.cost) : null}
      />
      <Insight
        label={t(($) => $.charts.heatmap_most_active_weekday)}
        value={
          busyDayIndex === null ? "—" : (weekdayLabels[busyDayIndex] ?? "—")
        }
        sub={busyDayIndex !== null
          ? t(($) => $.charts.heatmap_average, { value: formatUsd(busyDayAvg) })
          : null}
      />
      <Insight
        label={t(($) => $.charts.heatmap_quietest_weekday)}
        value={
          quietDayIndex === null ? "—" : (weekdayLabels[quietDayIndex] ?? "—")
        }
        sub={quietDayIndex !== null
          ? t(($) => $.charts.heatmap_average, { value: formatUsd(quietDayAvg) })
          : null}
      />
      <Insight
        label={t(($) => $.charts.heatmap_window_total, { count: windowDays })}
        value={formatUsd(totalCost)}
      />
    </dl>
  );
}

function Insight({
  label,
  value,
  sub,
}: {
  label: string;
  value: string;
  sub?: string | null;
}) {
  return (
    <div className="min-w-0">
      <dt className="truncate text-micro uppercase tracking-wider text-muted-foreground">
        {label}
      </dt>
      <dd className="mt-0.5 truncate text-body font-medium tabular-nums">
        {value}
        {sub != null && (
          <span className="ml-1.5 text-caption font-normal text-muted-foreground">
            {sub}
          </span>
        )}
      </dd>
    </div>
  );
}
