"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import {
  artifactsByIssueOptions,
  planVersionsOptions,
  selectIssuePlan,
  parseWorkpadPhases,
  namedPhases,
  workpadProgress,
  phaseStatus,
  type WorkpadItem,
  type WorkpadPhase,
} from "@multica/cerebro-artifacts/core";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { StatusIcon } from "@multica/views/issues/components";
import { cn } from "@multica/ui/lib/utils";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Separator } from "@multica/ui/components/ui/separator";
import { WorkpadProgressCircle } from "./workpad-progress-circle";

// StepList renders one flat run of checklist steps. `subdued` renders the steps
// one step lighter than the phase titles they sit under, so a phase's sub-steps
// read as detail below its status row.
function StepList({
  items,
  subdued,
}: {
  items: WorkpadItem[];
  subdued?: boolean;
}) {
  return (
    <ul className="flex flex-col gap-1.5">
      {items.map((item, i) => (
        <li key={i} className="flex items-start gap-2 text-sm">
          {/* A step's box is a read-only MARKER, not an input. It uses
              `readOnly` rather than `disabled` on purpose: `disabled` pulls in
              the shared Checkbox's `disabled:opacity-50`, which on top of the
              already-faint `border-input` left the empty boxes almost invisible
              against the panel's muted background. `readOnly` keeps full
              opacity, and the stronger border below gives the unchecked box a
              visible edge. `tabIndex={-1}` keeps this aria-hidden marker out of
              the tab order (a readOnly checkbox, unlike a disabled one, is
              still focusable). */}
          <Checkbox
            checked={item.done}
            readOnly
            tabIndex={-1}
            aria-hidden="true"
            className="mt-0.5 shrink-0 border-muted-foreground/60"
          />
          <span
            className={cn(
              "min-w-0 leading-snug",
              subdued && "text-muted-foreground",
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

// PhaseSection renders one named phase as a collapsible row: a status icon that
// reflects the phase's own progress (todo / in-progress / done), the phase
// title, and its step count, followed by its steps when expanded. The status row
// itself is the toggle — there is no separate disclosure arrow — so a long plan
// folds down to a handful of phase rows.
function PhaseSection({
  phase,
  open,
  onToggle,
}: {
  phase: WorkpadPhase;
  open: boolean;
  onToggle: () => void;
}) {
  const progress = workpadProgress(phase.items);
  return (
    <div className="flex flex-col gap-1.5">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className="group -mx-1 flex items-center gap-2 rounded-sm px-1 py-0.5 text-left hover:bg-muted focus:outline-none focus:ring-2 focus:ring-ring"
      >
        <StatusIcon
          status={phaseStatus(progress)}
          className="size-3.5 shrink-0"
        />
        <span className="min-w-0 truncate text-sm font-medium">
          {phase.title}
        </span>
        <span className="ml-auto shrink-0 text-xs tabular-nums text-muted-foreground">
          {progress.done}/{progress.total}
        </span>
      </button>
      {open && (
        <div className="pl-5">
          <StepList items={phase.items} subdued />
        </div>
      )}
    </div>
  );
}

// FIR-3659 — WorkpadPanel renders the issue's PLAN as a checklist at the very
// bottom of the issue, below the composer. It is driven entirely by the presence
// of a `kind: "plan"` artifact coupled to the issue: no plan → renders nothing;
// a plan exists → the panel appears automatically, with no agent action
// required. Clicking the title opens the plan note (where its version history
// lives). Gated by the cerebro_workpad feature flag.
//
// FIR-3765 — when the plan groups its steps under markdown headings (phases),
// each phase renders as a collapsible section headed by a status icon that
// reflects the phase's own progress (todo / in-progress / done). The status row
// is the toggle; a phase's sub-steps render one shade lighter beneath it. A plan
// with fewer than two named phases renders as a single flat list, exactly as
// before.
//
// Everything starts folded: the panel itself opens collapsed (the Workpad icon
// toggles it) and so does every phase, so the panel reads as a compact status
// line until the reader asks for detail.
//
// The header's own icon is a WorkpadProgressCircle — the same circle the phases
// use, but filled proportionally to the whole plan's progress, so the collapsed
// panel shows at a glance how far the plan has come. When expanded, a rule
// separates the heading from the plan title beneath it.
export function WorkpadPanel({ issueId, className }: { issueId: string; className?: string }) {
  const enabled = useFeatureFlag("cerebro_workpad");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const router = useNavigation();
  const [open, setOpen] = useState(false);
  // Per-phase open overrides (keyed by title). A phase not present here is
  // collapsed.
  const [openOverrides, setOpenOverrides] = useState<Record<string, boolean>>({});

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

  const showPhases = namedPhases(phases).length >= 2;

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
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          aria-expanded={open}
          aria-label="Toggle Workpad"
          title="Toggle Workpad"
          className="-m-1 shrink-0 rounded-sm p-1 hover:opacity-80 focus:outline-none focus:ring-2 focus:ring-ring"
        >
          <WorkpadProgressCircle progress={{ done, total }} className="size-4" />
        </button>
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

      {open && (
        <>
          {/* A rule separates the Workpad heading from the plan it is showing,
              so the expanded panel reads as a titled section instead of two
              stacked lines. */}
          <Separator className="mt-2" />
          <p className="mt-2 flex items-center gap-1.5 text-xs text-muted-foreground">
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
          ) : showPhases ? (
            <div className="mt-2 flex flex-col gap-2">
              {phases.map((phase, i) => {
                if (phase.title === null) {
                  return <StepList key={`__lead__${i}`} items={phase.items} />;
                }
                const title = phase.title;
                const phaseOpen = openOverrides[title] ?? false;
                return (
                  <PhaseSection
                    key={title}
                    phase={phase}
                    open={phaseOpen}
                    onToggle={() =>
                      setOpenOverrides((o) => ({ ...o, [title]: !phaseOpen }))
                    }
                  />
                );
              })}
            </div>
          ) : (
            <div className="mt-2">
              <StepList items={items} />
            </div>
          )}
        </>
      )}
    </div>
  );
}
