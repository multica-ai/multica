"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ListChecks, ExternalLink } from "lucide-react";
import {
  artifactsByIssueOptions,
  planVersionsOptions,
  selectIssuePlan,
  parseWorkpadPhases,
  namedPhases,
  workpadProgress,
  type WorkpadItem,
  type WorkpadPhase,
} from "@multica/cerebro-artifacts/core";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { cn } from "@multica/ui/lib/utils";
import { Checkbox } from "@multica/ui/components/ui/checkbox";

const ALL = "__all__";

// StepList renders one flat run of checklist steps.
function StepList({ items }: { items: WorkpadItem[] }) {
  return (
    <ul className="flex flex-col gap-1.5">
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
  );
}

// PhaseBlock renders one phase: its heading (with per-phase progress) followed
// by its steps. A null-title phase (steps before the first heading) renders
// just the steps, with no heading.
function PhaseBlock({ phase }: { phase: WorkpadPhase }) {
  const { done, total } = workpadProgress(phase.items);
  return (
    <div className="flex flex-col gap-1.5">
      {phase.title !== null && (
        <div className="flex items-center gap-2">
          <span className="text-xs font-semibold text-muted-foreground">
            {phase.title}
          </span>
          <span className="text-xs tabular-nums text-muted-foreground/70">
            {done}/{total}
          </span>
        </div>
      )}
      <StepList items={phase.items} />
    </div>
  );
}

// PhaseChip is a small pill used to pick which phase the panel shows.
function PhaseChip({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "rounded-full border px-2 py-0.5 text-xs transition-colors focus:outline-none focus:ring-2 focus:ring-ring",
        active
          ? "border-transparent bg-primary text-primary-foreground"
          : "border-border text-muted-foreground hover:bg-muted",
      )}
    >
      {label}
    </button>
  );
}

// FIR-3659 — WorkpadPanel renders the issue's PLAN as a checklist directly
// above the composer. It is driven entirely by the presence of a `kind: "plan"`
// artifact coupled to the issue: no plan → renders nothing; a plan exists → the
// panel appears automatically, with no agent action required. Clicking the
// title opens the plan note (where its version history lives). Gated by the
// cerebro_workpad feature flag.
//
// FIR-3765 — when the plan groups its steps under markdown headings (phases),
// the panel renders them as phase blocks and shows a filter so a long plan can
// be viewed as "Alle" or one phase at a time. A plan with fewer than two named
// phases renders as a single flat list, exactly as before.
export function WorkpadPanel({ issueId, className }: { issueId: string; className?: string }) {
  const enabled = useFeatureFlag("cerebro_workpad");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const router = useNavigation();
  const [selected, setSelected] = useState<string>(ALL);

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

  const phases = parseWorkpadPhases(plan.body);
  const items = phases.flatMap((p) => p.items);
  const { done, total } = workpadProgress(items);
  const versionCount = versions?.length ?? 0;

  const named = namedPhases(phases);
  const showSelector = named.length >= 2;
  // Filter to the selected phase (by title); fall back to all phases when the
  // selection is stale (e.g. the plan changed and the title no longer exists).
  const filtered =
    showSelector && selected !== ALL
      ? phases.filter((p) => p.title === selected)
      : phases;
  const visible = filtered.length > 0 ? filtered : phases;

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

      {showSelector && (
        <div
          role="group"
          aria-label="Filter plan by phase"
          className="mt-2 flex flex-wrap items-center gap-1"
        >
          <PhaseChip
            label="Alle"
            active={selected === ALL}
            onClick={() => setSelected(ALL)}
          />
          {named.map((p) => (
            <PhaseChip
              key={p.title as string}
              label={p.title as string}
              active={selected === p.title}
              onClick={() => setSelected(p.title as string)}
            />
          ))}
        </div>
      )}

      {total === 0 ? (
        <p className="mt-2 text-xs text-muted-foreground">
          The plan has no steps yet.
        </p>
      ) : (
        <div className="mt-2 flex flex-col gap-3">
          {visible.map((phase, i) => (
            <PhaseBlock key={phase.title ?? `__lead__${i}`} phase={phase} />
          ))}
        </div>
      )}
    </div>
  );
}
