import { useMemo } from "react";
import type { RuntimeUsage } from "@multica/core/types";
import { estimateCost } from "../../utils";
import { useT, useLocale } from "@multica/i18n/react";

// 26 weeks (~6 months) gives the heatmap real presence in the wider chart
// card and turns "long-view" into a meaningful tab — a 13-week strip looked
// cramped. Cells at 16px (vs GitHub's 11) keep the calendar-square density
// readable at this scale.
const HEATMAP_WEEKS = 26;
const CELL_SIZE = 16;
const CELL_GAP = 3;

// Parse comma-separated i18n strings into arrays.
// Default fallback values match the original English hardcoded labels.
function parseDayLabels(t: (key: string, params?: Record<string, string | number>) => string): string[] {
  const raw = t("heatmap_day_labels");
  return raw.split(",");
}
function parseWeekdayNames(t: (key: string, params?: Record<string, string | number>) => string): string[] {
  const raw = t("heatmap_weekday_names");
  return raw.split(",");
}

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

function fmtMoney(n: number): string {
  if (n >= 100) return `$${n.toFixed(0)}`;
  return `$${n.toFixed(2)}`;
}

function fmtDate(iso: string, locale: string): string {
  return new Date(iso + "T00:00:00").toLocaleString(locale, {
    month: "short",
    day: "numeric",
  });
}

interface Insights {
  busiestDay: { date: string; cost: number } | null;
  busyDayName: string | null;
  busyDayAvg: number;
  quietDayName: string | null;
  quietDayAvg: number;
  totalCost: number;
  windowDays: number;
}

export function ActivityHeatmap({ usage }: { usage: RuntimeUsage[] }) {
  const t = useT("runtimes");
  const { locale } = useLocale();
  const dayLabels = parseDayLabels(t);
  const weekdayNames = parseWeekdayNames(t);
  const { cells, monthLabels, insights } = useMemo(() => {
    // Sum priced cost per day. Cost (not tokens) gives the colour scale a
    // financial meaning that lines up with the rest of the page — a "hot"
    // square here means the same thing as a tall bar in Daily cost.
    const dateCost = new Map<string, number>();
    for (const u of usage) {
      dateCost.set(u.date, (dateCost.get(u.date) ?? 0) + estimateCost(u));
    }

    const today = new Date();
    const todayDay = today.getDay();
    const startOffset = todayDay + (HEATMAP_WEEKS - 1) * 7;
    const startDate = new Date(today);
    startDate.setDate(today.getDate() - startOffset);

    const allCells: {
      date: string;
      dayOfWeek: number;
      week: number;
      cost: number;
    }[] = [];
    const d = new Date(startDate);
    for (let i = 0; i <= startOffset; i++) {
      const dateStr = d.toISOString().slice(0, 10);
      const dayOfWeek = d.getDay();
      const week = Math.floor(i / 7);
      allCells.push({
        date: dateStr,
        dayOfWeek,
        week,
        cost: dateCost.get(dateStr) ?? 0,
      });
      d.setDate(d.getDate() + 1);
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
    let busyDayName: string | null = null;
    let busyDayAvg = 0;
    let quietDayName: string | null = null;
    let quietDayAvg = Number.POSITIVE_INFINITY;
    weekdayAvg.forEach((avg, i) => {
      const name = weekdayNames[i] ?? "";
      if (avg > busyDayAvg) {
        busyDayAvg = avg;
        busyDayName = name;
      }
      if (avg < quietDayAvg) {
        quietDayAvg = avg;
        quietDayName = name;
      }
    });
    if (quietDayAvg === Number.POSITIVE_INFINITY) quietDayAvg = 0;
    // When the window has no spend at all, the busy / quiet weekday picks
    // are noise (every weekday averaged to 0). Suppress them.
    if (totalCost === 0) {
      busyDayName = null;
      quietDayName = null;
    }

    const insights: Insights = {
      busiestDay,
      busyDayName,
      busyDayAvg,
      quietDayName,
      quietDayAvg,
      totalCost,
      windowDays: allCells.length,
    };

    return { cells: cellsWithLevel, monthLabels: months, insights };
  }, [usage, locale, weekdayNames]);

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
            {dayLabels.map((label, i) =>
              label ? (
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
                <title>
                  {c.date}:{" "}
                  {c.cost > 0 ? `$${c.cost.toFixed(2)}` : t("heatmap_no_activity")}
                </title>
              </rect>
            ))}
          </svg>
        </div>
        <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
          <span>{t("heatmap_less")}</span>
          {[0, 1, 2, 3, 4].map((level) => (
            <div
              key={level}
              className="h-[10px] w-[10px] rounded-[2px]"
              style={{ backgroundColor: getHeatmapColor(level) }}
            />
          ))}
          <span>{t("heatmap_more")}</span>
        </div>
      </div>

      <InsightsRow insights={insights} />
    </div>
  );
}

// Horizontal stat strip beneath the heatmap. Mirrors the page-top KPI
// hero pattern (label → big value → sub) but at smaller scale to stay
// secondary. 4 columns on desktop, 2 on narrow screens.
function InsightsRow({ insights }: { insights: Insights }) {
  const t = useT("runtimes");
  const { locale } = useLocale();
  const {
    busiestDay,
    busyDayName,
    busyDayAvg,
    quietDayName,
    quietDayAvg,
    totalCost,
    windowDays,
  } = insights;
  const avgPrefix = t("heatmap_avg_prefix");
  return (
    <dl className="grid grid-cols-2 gap-x-4 gap-y-3 border-t pt-3 sm:grid-cols-4">
      <Insight
        label={t("heatmap_insight_busiest_day")}
        value={busiestDay ? fmtDate(busiestDay.date, locale) : "—"}
        sub={busiestDay ? fmtMoney(busiestDay.cost) : null}
      />
      <Insight
        label={t("heatmap_insight_active_weekday")}
        value={busyDayName ?? "—"}
        sub={busyDayName ? `${avgPrefix} ${fmtMoney(busyDayAvg)}` : null}
      />
      <Insight
        label={t("heatmap_insight_quiet_weekday")}
        value={quietDayName ?? "—"}
        sub={quietDayName ? `${avgPrefix} ${fmtMoney(quietDayAvg)}` : null}
      />
      <Insight label={t("heatmap_insight_period_total", { days: windowDays })} value={fmtMoney(totalCost)} />
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
      <dt className="truncate text-[11px] uppercase tracking-wider text-muted-foreground">
        {label}
      </dt>
      <dd className="mt-0.5 truncate text-sm font-medium tabular-nums">
        {value}
        {sub != null && (
          <span className="ml-1.5 text-xs font-normal text-muted-foreground">
            {sub}
          </span>
        )}
      </dd>
    </div>
  );
}
