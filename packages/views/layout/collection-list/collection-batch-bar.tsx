"use client";

import type { ReactNode } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { X } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { useT } from "../../i18n";

/**
 * Floating bar for actions on the selected rows. `absolute` centres it on the
 * nearest positioned ancestor (the page root, so a sidebar or split pane does
 * not pull it off the list's centre); `fixed` centres it on the viewport;
 * `inline` leaves it in flow.
 */
export function CollectionBatchBar({
  count,
  onClear,
  placement = "absolute",
  label,
  children,
}: {
  count: number;
  onClear: () => void;
  placement?: "absolute" | "fixed" | "inline";
  /** Overrides the "N selected" text. */
  label?: ReactNode;
  children: ReactNode;
}) {
  const { t } = useT("layout");
  const reduceMotion = useReducedMotion() ?? false;
  const offset = reduceMotion ? "translateY(0)" : "translateY(8px)";

  return (
    <AnimatePresence initial={false}>
      {count > 0 && (
        <div
          key="collection-batch-bar"
          className={cn(
            "z-50",
            placement === "inline"
              ? "mb-2 w-fit"
              : "bottom-6 left-1/2 -translate-x-1/2 max-md:above-chat-launcher",
            placement === "absolute" && "absolute",
            placement === "fixed" && "fixed",
          )}
        >
          <motion.div
            role="toolbar"
            aria-label={t(($) => $.collection.selected, { count })}
            className="flex items-center gap-1 rounded-lg border bg-background px-2 py-1.5 shadow-lg"
            initial={{ opacity: 0, transform: offset }}
            animate={{
              opacity: 1,
              transform: "translateY(0)",
              transition: { duration: UI_MOTION_DURATION.fast, ease: UI_EASE_OUT },
            }}
            exit={{
              opacity: 0,
              transform: offset,
              transition: {
                duration: reduceMotion ? UI_MOTION_DURATION.fast : UI_MOTION_DURATION.micro,
                ease: UI_EASE_OUT,
              },
            }}
          >
            <div className="mr-1 flex items-center gap-1.5 border-r pl-1 pr-2">
              <span className="whitespace-nowrap text-body font-medium">
                {label ?? t(($) => $.collection.selected, { count })}
              </span>
              <TooltipHint label={t(($) => $.collection.clear_selection)}>
                <button
                  type="button"
                  aria-label={t(($) => $.collection.clear_selection)}
                  onClick={onClear}
                  className="rounded-xs p-0.5 transition-colors hover:bg-accent"
                >
                  <X className="size-3.5 text-muted-foreground" />
                </button>
              </TooltipHint>
            </div>
            {children}
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
}
