import { useEffect, useState } from "react";
import { ArrowDownToLine, ArrowUpToLine } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

// ---------------------------------------------------------------------------
// TimelineScrollButtons — back-to-top / jump-to-bottom for the issue detail
// ---------------------------------------------------------------------------
//
// On a long timeline (hundreds of comments) dragging from one end of the
// document to the other costs seconds of scrollbar travel. These two buttons
// sit at the right edge of the scroll viewport, on the same strip as the
// thread-minimap rail and the find bar, and jump the timeline to its top or
// bottom in one press (upstream #6959).
//
// Each button hides while the timeline already rests within ~one screen of
// that end — the affordance only appears when it saves a trip. The buttons
// stay mounted and only fade (opacity + pointer-events), so appearing never
// reflows the rail beside them.
//
// The buttons never scroll anything themselves: they call back into
// issue-detail, which owns both scroll backends (Virtuoso index jumps for
// the virtualized mode, container scrollTop for the flat mode) and must
// never use native scrollIntoView on the main container (#3929).

interface TimelineScrollButtonsProps {
  /** The issue detail scroll container; null until its callback ref populates. */
  scrollContainerEl: HTMLElement | null;
  onScrollToTop: () => void;
  onScrollToBottom: () => void;
  /** Positioning within the page — owned by the caller, like ThreadMinimap. */
  className?: string;
}

/** Whether the timeline rests within one viewport of a scroll end. */
function useNearScrollEnds(
  scrollContainerEl: HTMLElement | null,
): { nearTop: boolean; nearBottom: boolean } {
  const [near, setNear] = useState({ nearTop: true, nearBottom: true });

  useEffect(() => {
    const container = scrollContainerEl;
    if (!container) return;

    let raf = 0;
    const compute = () => {
      raf = 0;
      // Threshold is one viewport: closer than that to an end and the
      // scrollbar itself is already the faster instrument.
      const threshold = container.clientHeight;
      const maxScroll = container.scrollHeight - container.clientHeight;
      const top = container.scrollTop;
      const nearTop = top <= threshold;
      const nearBottom = maxScroll - top <= threshold;
      setNear((prev) =>
        prev.nearTop === nearTop && prev.nearBottom === nearBottom
          ? prev
          : { nearTop, nearBottom },
      );
    };
    const schedule = () => {
      if (!raf) raf = requestAnimationFrame(compute);
    };

    compute();
    container.addEventListener("scroll", schedule, { passive: true });
    // Content height changes without scroll events: Virtuoso mounting rows
    // after first paint, streamed replies growing, window resizes.
    const ro = new ResizeObserver(schedule);
    ro.observe(container);
    if (container.firstElementChild) ro.observe(container.firstElementChild);
    return () => {
      container.removeEventListener("scroll", schedule);
      ro.disconnect();
      if (raf) cancelAnimationFrame(raf);
    };
  }, [scrollContainerEl]);

  return near;
}

function JumpButton({
  label,
  hidden,
  onClick,
  children,
}: {
  label: string;
  /** Faded out: the timeline already sits at this end. */
  hidden: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="outline"
            size="icon-sm"
            aria-label={label}
            // Hidden state keeps the node (no reflow for the rail beside it)
            // but drops it from the a11y tree and the tab order, matching the
            // visual "not here" reading.
            aria-hidden={hidden || undefined}
            tabIndex={hidden ? -1 : undefined}
            onClick={onClick}
            className={cn(
              "bg-background/95 shadow-[var(--floating-shadow)] transition-opacity duration-150",
              hidden && "pointer-events-none opacity-0",
            )}
          >
            {children}
          </Button>
        }
      />
      <TooltipContent side="left">{label}</TooltipContent>
    </Tooltip>
  );
}

export function TimelineScrollButtons({
  scrollContainerEl,
  onScrollToTop,
  onScrollToBottom,
  className,
}: TimelineScrollButtonsProps) {
  const { t } = useT("issues");
  const { nearTop, nearBottom } = useNearScrollEnds(scrollContainerEl);

  return (
    <div className={cn("flex flex-col gap-1", className)}>
      <JumpButton
        label={t(($) => $.detail.scroll_to_top)}
        hidden={nearTop}
        onClick={onScrollToTop}
      >
        <ArrowUpToLine />
      </JumpButton>
      <JumpButton
        label={t(($) => $.detail.scroll_to_bottom)}
        hidden={nearBottom}
        onClick={onScrollToBottom}
      >
        <ArrowDownToLine />
      </JumpButton>
    </div>
  );
}
