"use client";

// Phase 3 — single chip button on a PR card.
//
// The button wraps the Button shadcn primitive with three behaviours layered
// on top:
//   1. Click → fires the supplied mutation. Destructive chips first open an
//      AlertDialog confirmation; the dialog reuses the same translations
//      under `ship.chips.<action>.confirm_*`.
//   2. Pending state shows the Spinner inline with the icon.
//   3. Settled handlers translate the ActionResult.status into a toast:
//      succeeded → checkmark + success toast; in_progress → "agent working
//      on it" with the task id as a hint; failed → error toast with the
//      server's error message.
//
// IMPORTANT: anchor wrapping. ShipPRCard is itself an `<a href={pr.html_url}>`
// and the chips render INSIDE it — clicking a chip would otherwise navigate
// to GitHub. Every interactive event handler here calls
// stopPropagation()+preventDefault() so the chip swallows the click before
// the parent anchor sees it. (Fix for the kind of nested-anchor bug we
// shipped earlier in #2143-style incidents.)

import { useState, type MouseEvent, type SyntheticEvent } from "react";
import { Check } from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { cn } from "@multica/ui/lib/utils";
import type { ActionResult, PullRequest } from "@multica/core/types";
import { useT } from "../../i18n";
import type { PrChip } from "../hooks/use-pr-chips";
import {
  chipLabel,
  chipSuccessToast,
  chipInProgressToast,
  chipConfirmTitle,
  chipConfirmDescription,
  chipConfirmAction,
} from "./chip-strings";

interface ChipButtonProps {
  chip: PrChip;
  pr: PullRequest;
  /** Mutation function — already bound to a specific PR id by the parent.
   *  Returning the parsed ActionResult lets us branch on `status` after
   *  awaiting. */
  onFire: (body?: Record<string, unknown>) => Promise<ActionResult>;
  /** True while the bound mutation is in flight; passed in by the parent
   *  rather than read from a hook so the parent can disable a whole row of
   *  chips during an action (avoids double-fire when two chips touch the
   *  same PR). */
  isPending: boolean;
}

// Stops the click from reaching the parent <a href> on the card. Used on
// every interactive surface inside the dialog and the chip itself.
function swallow(e: SyntheticEvent) {
  e.stopPropagation();
  if ("preventDefault" in e) (e as MouseEvent).preventDefault();
}

export function ChipButton({ chip, pr, onFire, isPending }: ChipButtonProps) {
  const { t } = useT("ship");
  // Local "just succeeded" state — drives the brief checkmark animation.
  // Reset by the timeout below so a second press doesn't show a stale check.
  const [showCheckmark, setShowCheckmark] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const Icon = chip.icon;

  const label = chipLabel(t, chip.action);

  // Map our PrChipVariant to the shadcn Button variant. "primary" maps to
  // the default solid; "secondary" to outline so two visually distinct
  // chips can coexist (primary + secondary side by side); "destructive" to
  // the red destructive variant. The dialog confirm button below uses the
  // chip's variant so the user sees a coherent color story across click +
  // confirm.
  const buttonVariant: "default" | "outline" | "destructive" =
    chip.variant === "primary"
      ? "default"
      : chip.variant === "destructive"
        ? "destructive"
        : "outline";

  const fire = async (e: MouseEvent | SyntheticEvent) => {
    swallow(e);
    try {
      const body = chip.bodyBuilder?.(pr);
      const result = await onFire(body);
      if (result.status === "succeeded") {
        setShowCheckmark(true);
        // Reset the checkmark after a beat. Long enough for the user to
        // notice; short enough the chip is back to actionable quickly.
        setTimeout(() => setShowCheckmark(false), 1200);
        toast.success(chipSuccessToast(t, chip.action));
      } else if (result.status === "in_progress") {
        toast.info(chipInProgressToast(t, chip.action), {
          description: result.agent_task_id
            ? t(($) => $.chips.task_id_hint, { id: result.agent_task_id })
            : undefined,
        });
      } else {
        toast.error(
          result.error || t(($) => $.chips.toast_generic_failure),
        );
      }
    } catch (err) {
      // Non-2xx responses raise an ApiError whose `.message` is the parsed
      // server `error` field — surface it directly so the user sees what
      // GitHub said (e.g. "branch is not mergeable", "review_id is required").
      toast.error(
        err instanceof Error
          ? err.message
          : t(($) => $.chips.toast_generic_failure),
      );
    }
  };

  const handleClick = (e: MouseEvent) => {
    swallow(e);
    if (chip.destructive) {
      setConfirmOpen(true);
      return;
    }
    void fire(e);
  };

  // Visual state: pending wins over success animation, both win over the
  // chip's normal icon.
  const iconNode = isPending ? (
    <Spinner className="size-3" aria-hidden />
  ) : showCheckmark ? (
    <Check className="size-3" aria-hidden />
  ) : (
    <Icon className="size-3" aria-hidden />
  );

  return (
    <>
      <Button
        type="button"
        size="xs"
        variant={buttonVariant}
        disabled={isPending}
        onClick={handleClick}
        // Keyboard activation also needs to swallow up the parent anchor.
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") swallow(e);
        }}
        className={cn(
          "h-6 px-2 text-xs",
          // Subtle scale animation on the checkmark frame so the "done"
          // affordance reads as deliberate rather than a flash.
          showCheckmark && "scale-105 transition-transform",
        )}
        aria-label={label}
      >
        {iconNode}
        <span className="truncate">{label}</span>
      </Button>

      {chip.destructive && (
        <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
          <AlertDialogContent
            // Stop dialog clicks from reaching the parent card anchor.
            onClick={swallow}
            onKeyDown={swallow}
          >
            <AlertDialogHeader>
              <AlertDialogTitle>
                {chipConfirmTitle(t, chip.action)}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {chipConfirmDescription(t, chip.action, {
                  number: pr.number,
                  title: pr.title,
                })}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel onClick={swallow}>
                {t(($) => $.chips.confirm_cancel)}
              </AlertDialogCancel>
              <AlertDialogAction
                variant={
                  chip.variant === "destructive" ? "destructive" : "default"
                }
                onClick={(e) => {
                  setConfirmOpen(false);
                  void fire(e);
                }}
              >
                {chipConfirmAction(t, chip.action)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </>
  );
}
