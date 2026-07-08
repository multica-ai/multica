import { useState } from "react";
import { ChevronRight } from "lucide-react";
import type { IssueTaskUsage, IssueUsageSummary } from "@multica/core/types";
import { PropRow } from "../../common/prop-row";
import { useT } from "../../i18n";
import { estimateCost } from "../../runtimes/utils";

export function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

// One run can span multiple models (one task_usage row per model); fold the
// rows back into per-run entries for the breakdown list.
interface RunEntry {
  taskId: string;
  createdAt: string;
  commentTriggered: boolean;
  tokens: number;
  cost: number;
}

function foldRuns(tasks: IssueTaskUsage[]): RunEntry[] {
  const byTask = new Map<string, RunEntry>();
  for (const t of tasks) {
    const entry = byTask.get(t.task_id) ?? {
      taskId: t.task_id,
      createdAt: t.created_at,
      commentTriggered: t.comment_triggered,
      tokens: 0,
      cost: 0,
    };
    entry.tokens +=
      t.input_tokens + t.output_tokens + t.cache_read_tokens + t.cache_write_tokens;
    entry.cost += estimateCost(t);
    byTask.set(t.task_id, entry);
  }
  return [...byTask.values()];
}

function formatCost(cost: number): string {
  return cost >= 0.01 ? `$${cost.toFixed(2)}` : `$${cost.toFixed(4)}`;
}

export function IssueTokenUsageSection({ usage }: { usage: IssueUsageSummary }) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(true);
  const [runsOpen, setRunsOpen] = useState(false);

  if (usage.task_count === 0) return null;

  const runs = foldRuns(usage.tasks);
  const totalCost = runs.reduce((sum, r) => sum + r.cost, 0);

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.detail.section_token_usage)}
        <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 pl-2">
          {totalCost > 0 && (
            <PropRow label={t(($) => $.detail.prop_est_cost)}>
              <span className="text-muted-foreground">{formatCost(totalCost)}</span>
            </PropRow>
          )}
          <PropRow label={t(($) => $.detail.prop_input)}>
            <span className="text-muted-foreground">{formatTokenCount(usage.total_input_tokens)}</span>
          </PropRow>
          <PropRow label={t(($) => $.detail.prop_output)}>
            <span className="text-muted-foreground">{formatTokenCount(usage.total_output_tokens)}</span>
          </PropRow>
          {(usage.total_cache_read_tokens > 0 || usage.total_cache_write_tokens > 0) && (
            <PropRow
              label={t(($) => $.detail.prop_cache)}
              title={t(($) => $.detail.prop_cache_tooltip)}
            >
              <span className="text-muted-foreground">
                {t(($) => $.detail.prop_cache_value, {
                  read: formatTokenCount(usage.total_cache_read_tokens),
                  write: formatTokenCount(usage.total_cache_write_tokens),
                })}
              </span>
            </PropRow>
          )}
          {runs.length > 0 ? (
            <>
              <button
                type="button"
                className="col-span-2 flex items-center gap-1 text-left text-xs text-muted-foreground hover:text-foreground"
                onClick={() => setRunsOpen(!runsOpen)}
              >
                {t(($) => $.detail.usage_runs_toggle, { count: usage.task_count })}
                <ChevronRight className={`!size-3 shrink-0 transition-transform ${runsOpen ? "rotate-90" : ""}`} />
              </button>
              {runsOpen &&
                runs.map((run) => (
                  <PropRow
                    key={run.taskId}
                    label={
                      run.commentTriggered
                        ? t(($) => $.detail.usage_run_comment)
                        : t(($) => $.detail.usage_run_assignment)
                    }
                  >
                    <span className="truncate text-muted-foreground">
                      {formatTokenCount(run.tokens)}
                      {run.cost > 0 && ` · ${formatCost(run.cost)}`}
                    </span>
                  </PropRow>
                ))}
            </>
          ) : (
            <PropRow label={t(($) => $.detail.prop_runs)}>
              <span className="text-muted-foreground">{usage.task_count}</span>
            </PropRow>
          )}
        </div>
      )}
    </div>
  );
}
