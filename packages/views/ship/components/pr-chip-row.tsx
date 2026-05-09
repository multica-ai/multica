"use client";

// Phase 3 — chip row beneath the PR card body.
//
// Owns:
//   - One mutation hook per chip action (only the hooks for chips this PR
//     actually qualifies for would mount, but React rules-of-hooks force
//     us to call all of them unconditionally; the mutations are cheap until
//     fired).
//   - Picking the right mutation for each chip from the union and binding
//     its mutateAsync as the `onFire` callback the ChipButton needs.
//   - Rendering at most the first 2 chips inline; everything else goes
//     into a "more actions" dropdown menu so the card height stays
//     bounded.
//
// We DON'T own:
//   - Toast/dialog UI — that's all inside ChipButton.
//   - Cache invalidation — the mutations themselves do that on settle.

import { useMemo } from "react";
import { MoreHorizontal } from "lucide-react";
import {
  useMergePullRequest,
  useRebasePullRequestOnMain,
  useDiagnoseCIFailure,
  useSummarizeReviewFeedback,
  useNudgePullRequestAuthor,
  useRunSmokeTests,
} from "@multica/core/ship";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Button } from "@multica/ui/components/ui/button";
import type { ActionResult, PullRequest } from "@multica/core/types";
import { useT } from "../../i18n";
import { derivePrChips, type PrChip } from "../hooks/use-pr-chips";
import { ChipButton } from "./chip-button";
import { chipLabel } from "./chip-strings";

interface PrChipRowProps {
  pr: PullRequest;
  /** Project's staging environment, when present. Drives the
   *  "Run smoke tests" chip. Pass null when the project hasn't configured
   *  staging yet. */
  stagingEnv?: { id: string; current_sha: string | null } | null;
  /** Cap on visible inline chips. Default 2 keeps the card compact. */
  maxVisible?: number;
}

// Same swallow-the-click guard used in ChipButton. Mirrored here for the
// dropdown trigger and items, both of which sit inside the parent <a>.
function swallow(e: { stopPropagation: () => void; preventDefault?: () => void }) {
  e.stopPropagation();
  e.preventDefault?.();
}

/** Bundle of chip mutation hooks. Each call returns mutateAsync + isPending;
 *  we wrap them up into a uniform shape the chip row can index by action. */
function useChipMutations(prId: string) {
  const merge = useMergePullRequest(prId);
  const rebase = useRebasePullRequestOnMain(prId);
  const diagnose = useDiagnoseCIFailure(prId);
  const summarize = useSummarizeReviewFeedback(prId);
  const nudge = useNudgePullRequestAuthor(prId);
  const smoke = useRunSmokeTests(prId);

  type FireFn = (body?: Record<string, unknown>) => Promise<ActionResult>;

  // The per-action firing functions are typed `FireFn`. Each backend chip
  // takes a different body shape; we cast the body via `unknown` here so
  // the row can pass the chip's bodyBuilder output uniformly. The schema
  // on the server still validates — the cast is purely a TS bridge.
  return useMemo(() => {
    const map: Record<string, { fire: FireFn; isPending: boolean }> = {
      merge: {
        fire: (body) => merge.mutateAsync(body as never),
        isPending: merge.isPending,
      },
      rebase_on_main: {
        fire: () => rebase.mutateAsync(),
        isPending: rebase.isPending,
      },
      diagnose_ci_failure: {
        fire: () => diagnose.mutateAsync(),
        isPending: diagnose.isPending,
      },
      summarize_review_feedback: {
        fire: () => summarize.mutateAsync(),
        isPending: summarize.isPending,
      },
      nudge_author: {
        fire: (body) => nudge.mutateAsync(body as never),
        isPending: nudge.isPending,
      },
      run_smoke_tests: {
        fire: (body) =>
          smoke.mutateAsync(
            (body ?? { environment_id: "" }) as { environment_id: string },
          ),
        isPending: smoke.isPending,
      },
    };
    return map;
  }, [merge, rebase, diagnose, summarize, nudge, smoke]);
}

export function PrChipRow({ pr, stagingEnv, maxVisible = 2 }: PrChipRowProps) {
  const { t } = useT("ship");
  const mutations = useChipMutations(pr.id);
  const chips = useMemo(
    () => derivePrChips(pr, { stagingEnv: stagingEnv ?? null }),
    [pr, stagingEnv],
  );

  if (chips.length === 0) return null;

  const visible = chips.slice(0, maxVisible);
  const overflow = chips.slice(maxVisible);

  // Are any chips for this PR currently firing? Used to disable the row
  // while in flight so a user can't queue multiple actions on the same
  // PR before the first one settles.
  const anyPending = visible.some(
    (c) => mutations[c.action]?.isPending,
  );

  const renderChip = (chip: PrChip) => {
    const m = mutations[chip.action];
    if (!m) return null;
    return (
      <ChipButton
        key={chip.id}
        chip={chip}
        pr={pr}
        onFire={m.fire}
        isPending={m.isPending || anyPending}
      />
    );
  };

  return (
    <div
      className="mt-2 flex flex-wrap items-center gap-1.5"
      // Stop hover/click events on the row's empty space from triggering
      // the parent <a> navigation. The visible buttons handle their own
      // events; this catches the gaps between chips.
      onClick={(e) => e.stopPropagation()}
    >
      {visible.map(renderChip)}
      {overflow.length > 0 && (
        <DropdownMenu>
          {/* Base UI's `<DropdownMenuTrigger>` accepts a `render` prop (not
              `asChild`) to swap the rendered element. We pass a Button so
              the affordance reads as another chip rather than a raw button
              with no styling. */}
          <DropdownMenuTrigger
            render={
              <Button
                type="button"
                size="xs"
                variant="ghost"
                className="h-6 w-6 p-0"
                onClick={swallow}
                aria-label={t(($) => $.chips.more_actions)}
              >
                <MoreHorizontal className="size-3" aria-hidden />
              </Button>
            }
          />
          <DropdownMenuContent
            align="end"
            // Dropdown sits inside the card anchor too — same click-swallow
            // discipline as the dialog inside ChipButton.
            onClick={swallow}
          >
            {overflow.map((chip) => {
              const m = mutations[chip.action];
              if (!m) return null;
              const Icon = chip.icon;
              const label = chipLabel(t, chip.action);
              return (
                <DropdownMenuItem
                  key={chip.id}
                  disabled={m.isPending}
                  onSelect={() => {
                    // For overflow chips we skip the confirmation dialog
                    // even when destructive — the dropdown is itself a
                    // deliberate two-step (open menu, click item) so a
                    // third confirmation reads as friction. Destructive
                    // actions in the inline row still confirm.
                    void m.fire(chip.bodyBuilder?.(pr));
                  }}
                >
                  <Icon className="size-3.5" aria-hidden />
                  {label}
                </DropdownMenuItem>
              );
            })}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  );
}
