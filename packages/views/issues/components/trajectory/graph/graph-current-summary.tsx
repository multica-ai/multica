import type { CurrentExecutionCategory, CurrentExecutionWindow } from "@multica/core/trajectory/current-execution";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../../../i18n";

export const CURRENT_CATEGORIES = ["running", "blocked", "review"] as const;

/** Current running, blocked and awaiting-review counts; each locates its issues. */
export function GraphCurrentSummary({ title, execution, available, locate, layout = "inline" }: {
  title: string;
  execution: CurrentExecutionWindow;
  available: boolean;
  locate: (category: CurrentExecutionCategory) => void;
  layout?: "inline" | "columns";
}) {
  const { t } = useT("issues");
  const label = (category: CurrentExecutionCategory) => {
    const count = execution.categories[category].length;
    return category === "running" ? t($ => $.trajectory_graph.current_running, { count })
      : category === "blocked" ? t($ => $.trajectory_graph.current_blocked, { count })
        : t($ => $.trajectory_graph.current_review, { count });
  };
  return <div className="trajectory-project-summary" data-layout={layout} role="group" aria-label={`${title} · ${t($ => $.trajectory_graph.current_window)}`}>
    {layout === "inline" && <span className="trajectory-summary-scope">{t($ => $.trajectory_graph.current_window)}</span>}
    {!available ? <span role="status" className="trajectory-summary-scope">{t($ => $.trajectory_graph.current_unavailable)}</span> : CURRENT_CATEGORIES.map(category => <Button key={category} variant="ghost" size="xs" className="trajectory-summary-category" data-current-state={category}
      disabled={!execution.categories[category].length} onClick={() => locate(category)}>
      <span className="trajectory-dot" aria-hidden="true"/>{label(category)}
    </Button>)}
  </div>;
}

/** Share of a project's issues per current category, for the collapsed list. */
export function GraphCurrentBar({ execution, total }: { execution: CurrentExecutionWindow; total: number }) {
  return <span className="trajectory-bar" aria-hidden="true">
    {CURRENT_CATEGORIES.map(category => <span key={category} data-current-state={category}
      style={{ width: total ? `${Math.round(execution.categories[category].length / total * 100)}%` : 0 }}/>)}
  </span>;
}
