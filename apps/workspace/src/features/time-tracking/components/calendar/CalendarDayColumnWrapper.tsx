/**
 * CalendarDayColumnWrapper — wraps each day column in the time grid.
 *
 * When `isNow` is true (today's column), injects an SVG play button positioned
 * exactly at the `.rbc-current-time-indicator` line. Uses MutationObserver +
 * ResizeObserver + setInterval to continuously sync the button's Y position.
 */
import React, { forwardRef, useRef, useLayoutEffect } from "react";

interface CalendarDayColumnWrapperProps {
  children?: React.ReactNode;
  className?: string;
  style?: React.CSSProperties;
  /** True when this column represents today. */
  isNow?: boolean;
  /** Called when the user clicks the play button to start a new entry. */
  onStartEntry?: () => void;
}

export const CalendarDayColumnWrapper = forwardRef<HTMLDivElement, CalendarDayColumnWrapperProps>(
  function CalendarDayColumnWrapper({ children, className, style, isNow, onStartEntry }, ref) {
    const columnRef = useRef<HTMLDivElement>(null);
    const playRef = useRef<SVGSVGElement>(null);

    // Merge external ref + internal ref.
    const setRef = (node: HTMLDivElement | null) => {
      (columnRef as React.MutableRefObject<HTMLDivElement | null>).current = node;
      if (typeof ref === "function") ref(node);
      else if (ref) (ref as React.MutableRefObject<HTMLDivElement | null>).current = node;
    };

    // Sync play button Y position with the current-time indicator.
    const syncPosition = () => {
      const indicator = columnRef.current?.querySelector<HTMLElement>(".rbc-current-time-indicator");
      if (indicator && playRef.current) {
        playRef.current.style.top = indicator.style.top;
      }
    };

    useLayoutEffect(() => {
      if (!isNow || !columnRef.current) return;

      const frame = requestAnimationFrame(syncPosition);
      const interval = window.setInterval(syncPosition, 10_000);

      const mutationObserver = new MutationObserver(syncPosition);
      mutationObserver.observe(columnRef.current, {
        attributes: true,
        childList: true,
        subtree: true,
      });

      let resizeObserver: ResizeObserver | null = null;
      if (typeof ResizeObserver !== "undefined") {
        resizeObserver = new ResizeObserver(syncPosition);
        resizeObserver.observe(columnRef.current);
      }

      return () => {
        cancelAnimationFrame(frame);
        window.clearInterval(interval);
        mutationObserver.disconnect();
        resizeObserver?.disconnect();
      };
    }, [isNow]);

    return (
      <div className={className} ref={setRef} style={style}>
        {children}
        {isNow && (
          <svg
            ref={playRef}
            fill="none"
            height="16"
            width="16"
            viewBox="0 0 36 36"
            xmlns="http://www.w3.org/2000/svg"
            style={{
              position: "absolute",
              left: "-7px",
              marginTop: "-6.5px",
              pointerEvents: "all",
              cursor: "pointer",
            }}
            onMouseDown={(e) => {
              // Prevent accidental drag start when clicking the play button.
              e.stopPropagation();
              e.preventDefault();
            }}
            onClick={(e) => {
              e.stopPropagation();
              onStartEntry?.();
            }}
          >
            <rect fill="var(--primary)" height="36" rx="18" width="36" />
            <path
              d="M13 11.994c0-1.101.773-1.553 1.745-.997l10.51 6.005c.964.55.972 1.439 0 1.994l-10.51 6.007c-.964.55-1.745.102-1.745-.997V11.994z"
              fill="var(--primary-foreground)"
            />
          </svg>
        )}
      </div>
    );
  },
);
