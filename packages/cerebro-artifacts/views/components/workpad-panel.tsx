"use client";

import { useQuery } from "@tanstack/react-query";
import { ListChecks, ExternalLink } from "lucide-react";
import { artifactsByIssueOptions, planVersionsOptions, selectIssuePlan, parseWorkpadChecklist, workpadProgress } from "@multica/cerebro-artifacts/core";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { cn } from "@multica/ui/lib/utils";
import { Checkbox } from "@multica/ui/components/ui/checkbox";

// FIR-3659 — WorkpadPanel renders the issue's PLAN as a checklist directly
// above the composer. It is driven entirely by the presence of a `kind: "plan"`
// artifact coupled to the issue: no plan → renders nothing; a plan exists → the
// panel appears automatically, with no agent action required. Clicking the
// title opens the plan note (where its version history lives). Gated by the
// cerebro_workpad feature flag.
export function WorkpadPanel({ issueId, className }: { issueId: string; className?: string }) {
  const enabled = useFeatureFlag("cerebro_workpad");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const router = useNavigation();

  const { data } = useQuery({
    ...artifactsByIssueOptions(wsId, issueId),
    enabled: Boolean(enabled && wsId && issueId),
  });

  // Computed from `data` (a pure select) BEFORE the version query so every hook
  // runs unconditionally — the version query stays disabled until a plan exists.
  const plan = selectIssuePlan(data);

  const { data: versions } = useQuery({
    ...planVersionsOptions(wsId, plan?.id ?? ""),
    enabled: Boolean(enabled && wsId && plan?.id),
  });

  if (!enabled) return null;
  if (!plan) return null;

  const items = parseWorkpadChecklist(plan.body);
  const { done, total } = workpadProgress(items);
  const versionCount = versions?.length ?? 0;

  const openPlan = () => {
    const path = paths.documentDetail(plan.id);
    if (router.openInNewTab) {
      router.openInNewTab(path, plan.title || "plan");
    } else {
      window.open(path, "_blank", "noopener,noreferrer");
    }
  };

  return (
    <div
      data-testid="workpad-panel"
      className={cn(
        "rounded-lg border border-border bg-muted/30 px-4 py-3",
        className,
      )}
    >
      <div className="flex items-center gap-2">
        <ListChecks className="size-4 shrink-0 text-muted-foreground" />
        <button
          type="button"
          onClick={openPlan}
          className="group inline-flex min-w-0 items-center gap-1 text-sm font-semibold hover:underline focus:outline-none focus:ring-2 focus:ring-ring rounded-sm"
          title="Open plan"
        >
          <span className="truncate">Workpad</span>
          <ExternalLink className="size-3 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
        </button>
        {total > 0 && (
          <span className="ml-auto text-xs tabular-nums text-muted-foreground">
            {done}/{total}
          </span>
        )}
      </div>

      <p className="mt-0.5 flex items-center gap-1.5 text-xs text-muted-foreground">
        {plan.title && <span className="truncate">{plan.title}</span>}
        {versionCount > 0 && (
          <>
            {plan.title && <span aria-hidden="true">·</span>}
            <button
              type="button"
              onClick={openPlan}
              className="shrink-0 tabular-nums hover:underline focus:outline-none focus:ring-2 focus:ring-ring rounded-sm"
              title="Open plan history"
            >
              {versionCount === 1 ? "1 version" : `${versionCount} versions`}
            </button>
          </>
        )}
      </p>

      {total === 0 ? (
        <p className="mt-2 text-xs text-muted-foreground">
          The plan has no steps yet.
        </p>
      ) : (
        <ul className="mt-2 flex flex-col gap-1.5">
          {items.map((item, i) => (
            <li key={i} className="flex items-start gap-2 text-sm">
              <Checkbox
                checked={item.done}
                disabled
                aria-hidden="true"
                className="mt-0.5 shrink-0"
              />
              <span
                className={cn(
                  "min-w-0 leading-snug",
                  item.done && "text-muted-foreground line-through",
                )}
              >
                {item.text}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
