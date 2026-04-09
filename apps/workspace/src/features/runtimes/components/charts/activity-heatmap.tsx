import { useMemo } from "react";
import type { RuntimeUsage } from "@/shared/types";
import { formatTokens } from "../../utils";

const HEATMAP_WEEKS = 13;
const CELL_SIZE = 11;
const CELL_GAP = 2;
const DAY_LABELS = ["", "Mon", "", "Wed", "", "Fri", ""];

function getHeatmapColor(level: number): string {
  const colors = [
    "var(--color-muted, hsl(var(--muted)))",
    "hsl(var(--chart-3) / 0.3)",
    "hsl(var(--chart-3) / 0.5)",
    "hsl(var(--chart-3) / 0.75)",
    "hsl(var(--chart-3) / 1)",
  ];
  return colors[level] ?? colors[0]!;
}

export function ActivityHeatmap({ usage }: { usage: RuntimeUsage[] }) {
  const { cells, monthLabels } = useMemo(() => {
    const dateTokens = new Map<string, number>();
    for (const u of usage) {
      const total =
        u.input_tokens + u.output_tokens + u.cache_read_tokens + u.cache_write_tokens;
      dateTokens.set(u.date, (dateTokens.get(u.date) ?? 0) + total);
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
      tokens: number;
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
        tokens: dateTokens.get(dateStr) ?? 0,
      });
      d.setDate(d.getDate() + 1);
    }

    const nonZero = allCells
      .filter((c) => c.tokens > 0)
      .map((c) => c.tokens);
    nonZero.sort((a, b) => a - b);
    const getLevel = (tokens: number) => {
      if (tokens === 0) return 0;
      if (nonZero.length <= 1) return 4;
      const p = nonZero.indexOf(tokens) / (nonZero.length - 1);
      if (p <= 0.25) return 1;
      if (p <= 0.5) return 2;
      if (p <= 0.75) return 3;
      return 4;
    };

    const cellsWithLevel = allCells.map((c) => ({
      ...c,
      level: getLevel(c.tokens),
    }));

    const months: { label: string; week: number }[] = [];
    let lastMonth = -1;
    for (const c of cellsWithLevel) {
      const month = new Date(c.date + "T00:00:00").getMonth();
      if (month !== lastMonth && c.dayOfWeek === 0) {
        months.push({
          label: new Date(c.date + "T00:00:00").toLocaleString("en", {
            month: "short",
          }),
          week: c.week,
        });
        lastMonth = month;
      }
    }

    return { cells: cellsWithLevel, monthLabels: months };
  }, [usage]);

  const labelWidth = 28;
  const svgWidth = labelWidth + HEATMAP_WEEKS * (CELL_SIZE + CELL_GAP);
  const svgHeight = 14 + 7 * (CELL_SIZE + CELL_GAP);

  return (
    <div className="rounded-lg border p-4">
      <h4 className="text-xs font-medium text-muted-foreground mb-3">Activity</h4>
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
          {DAY_LABELS.map((label, i) =>
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
              rx={2}
              fill={getHeatmapColor(c.level)}
              className="transition-colors"
            >
              <title>
                {c.date}:{" "}
                {c.tokens > 0
                  ? formatTokens(c.tokens) + " tokens"
                  : "No activity"}
              </title>
            </rect>
          ))}
        </svg>
      </div>
      {/* Legend */}
      <div className="mt-2 flex items-center justify-end gap-1 text-[10px] text-muted-foreground">
        <span>Less</span>
        {[0, 1, 2, 3, 4].map((level) => (
          <div
            key={level}
            className="h-[10px] w-[10px] rounded-[2px]"
            style={{ backgroundColor: getHeatmapColor(level) }}
          />
        ))}
        <span>More</span>
      </div>
    </div>
  );
}
