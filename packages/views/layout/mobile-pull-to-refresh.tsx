"use client";

import {
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { RefreshCw } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";

// Pull threshold in pixels — past this on touchend, onRefresh fires.
const THRESHOLD = 64;
// Visual height the indicator translates through; max pull distance with rubber-band.
const MAX_PULL = 96;

interface MobilePullToRefreshProps {
  children: ReactNode;
  onRefresh: () => Promise<unknown> | void;
  /** Tailwind classes for the scroll container. Must include overflow-y-auto + sizing. */
  className?: string;
}

// Touch-driven pull-to-refresh wrapper. Becomes the scroll container itself
// (so the children's overflow comes from this div). On desktop / non-touch
// the component is a passthrough — same div, same className, no listeners
// or extra DOM, so the desktop layout is identical to a plain div.
//
// We attach touchmove via addEventListener with passive:false so we can
// preventDefault during the pull and avoid iOS overscroll fighting our
// indicator. React's synthetic touchmove is passive by default and would
// silently ignore preventDefault.
export function MobilePullToRefresh({
  children,
  onRefresh,
  className,
}: MobilePullToRefreshProps) {
  const isMobile = useIsMobile();
  const containerRef = useRef<HTMLDivElement>(null);
  const startYRef = useRef<number | null>(null);
  const pullRef = useRef(0);
  const [pull, setPull] = useState(0);
  const [refreshing, setRefreshing] = useState(false);
  const refreshingRef = useRef(false);

  useEffect(() => {
    refreshingRef.current = refreshing;
  }, [refreshing]);

  useEffect(() => {
    if (!isMobile) return;
    const el = containerRef.current;
    if (!el) return;

    const onTouchStart = (e: TouchEvent) => {
      if (refreshingRef.current) return;
      if (el.scrollTop > 0) return;
      if (e.touches.length !== 1) return;
      const t = e.touches[0];
      if (!t) return;
      startYRef.current = t.clientY;
      pullRef.current = 0;
    };

    const onTouchMove = (e: TouchEvent) => {
      if (startYRef.current == null) return;
      if (refreshingRef.current) return;

      const t = e.touches[0];
      if (!t) return;
      const dy = t.clientY - startYRef.current;
      if (dy <= 0) {
        if (pullRef.current !== 0) {
          pullRef.current = 0;
          setPull(0);
        }
        return;
      }
      // Once the user starts a real pull, eat the touch so iOS doesn't
      // fight us with rubber-band overscroll. Doesn't run for tiny
      // jitters (dy <= 4) so casual finger drift doesn't lock scrolling.
      if (dy > 4) {
        e.preventDefault();
      }
      // Square-root rubber-band: linear early, decelerating after.
      const eased = Math.min(MAX_PULL, Math.sqrt(dy) * 9);
      pullRef.current = eased;
      setPull(eased);
    };

    const onTouchEnd = () => {
      if (startYRef.current == null) return;
      const finalPull = pullRef.current;
      startYRef.current = null;
      pullRef.current = 0;

      if (finalPull >= THRESHOLD) {
        setRefreshing(true);
        // Hold the indicator visible at threshold while refreshing.
        setPull(THRESHOLD);
        // Floor the visible refresh time so the spinner doesn't flash off in
        // 16ms when the cache invalidate resolves before paint. 320ms reads
        // as "yes I refreshed for you" without being annoyingly slow.
        const elapsed = performance.now();
        Promise.resolve(onRefresh()).finally(() => {
          const wait = Math.max(0, 320 - (performance.now() - elapsed));
          window.setTimeout(() => {
            setRefreshing(false);
            setPull(0);
          }, wait);
        });
      } else {
        setPull(0);
      }
    };

    el.addEventListener("touchstart", onTouchStart, { passive: true });
    el.addEventListener("touchmove", onTouchMove, { passive: false });
    el.addEventListener("touchend", onTouchEnd, { passive: true });
    el.addEventListener("touchcancel", onTouchEnd, { passive: true });
    return () => {
      el.removeEventListener("touchstart", onTouchStart);
      el.removeEventListener("touchmove", onTouchMove);
      el.removeEventListener("touchend", onTouchEnd);
      el.removeEventListener("touchcancel", onTouchEnd);
    };
  }, [isMobile, onRefresh]);

  if (!isMobile) {
    return <div className={className}>{children}</div>;
  }

  const indicatorOpacity = refreshing ? 1 : Math.min(1, pull / THRESHOLD);
  const indicatorRotation = refreshing ? 0 : Math.min(360, (pull / THRESHOLD) * 270);
  // Indicator sits 36px above the scroll viewport top; we translate it
  // down by `pull` so it animates into view as the finger pulls.
  return (
    <div ref={containerRef} className={cn("relative", className)}>
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-x-0 z-10 flex items-center justify-center"
        style={{
          top: -36,
          height: 36,
          transform: `translate3d(0, ${pull}px, 0)`,
          opacity: indicatorOpacity,
          transition:
            pull === 0 && !refreshing
              ? // Snap-back is the moment users notice abruptness, so push
                // it long enough to read as "easing", not "snap". 360ms with
                // iOS sheet decel is the same envelope the system uses for
                // bottom-sheet dismiss.
                "transform 0.36s cubic-bezier(0.32, 0.72, 0, 1), opacity 0.28s ease-out"
              : "none",
        }}
      >
        <div className="rounded-full bg-background/90 p-1.5 shadow-sm ring-1 ring-border">
          <RefreshCw
            className={cn("size-4 text-muted-foreground", refreshing && "animate-spin")}
            style={refreshing ? undefined : { transform: `rotate(${indicatorRotation}deg)` }}
          />
        </div>
      </div>
      {children}
    </div>
  );
}
