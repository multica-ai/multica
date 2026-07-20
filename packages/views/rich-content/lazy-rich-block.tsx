"use client";

/**
 * LazyRichBlock — near-viewport, stable-size mount gate for rich blocks
 * (MUL-4922 performance contract).
 *
 * A long chat session or a long comment thread can contain dozens of Mermaid
 * diagrams and sandboxed HTML iframes. Instantiating them all at once costs a
 * Mermaid render (async, layout-heavy) and an iframe (its own document +
 * script execution) per block, whether or not the user ever scrolls to them.
 * This shell defers each rich leaf until it is near the viewport.
 *
 * Two properties make this safe inside Virtuoso's virtualized list:
 *
 * 1. STABLE SIZE. The shell reserves the block's expected height BEFORE mounting
 *    and keeps that reservation as a `min-height` afterwards. Without it, a
 *    block would measure 0px while off-screen and jump to its real height on
 *    mount — which is precisely the measurement churn that makes a virtualized
 *    list mis-estimate item sizes and lose scroll position. The reservation is
 *    not a guess local to this file: it comes from the leaf components
 *    themselves (a session-cached real diagram height when available, else their
 *    documented skeleton/iframe height), so a cache hit mounts with zero shift.
 *
 * 2. MOUNT-ONCE LATCH. Once mounted the block is never unmounted, even when
 *    scrolled far away. Unmounting would re-run Mermaid and rebuild the iframe
 *    on every pass, and would discard the viewer's pan/zoom state — trading a
 *    one-time cost for a repeated one. Memory is bounded by "blocks the user
 *    actually scrolled past", not by the whole transcript.
 */

import { useEffect, useRef, useState, type ReactNode } from "react";

/**
 * How far outside the viewport a block starts mounting. Sized to cover
 * Virtuoso's own overscan (`increaseViewportBy` is 400px top / 600px bottom in
 * the chat list) so a block is ready by the time it is scrolled into view,
 * without eagerly building the whole transcript.
 */
const NEAR_VIEWPORT_ROOT_MARGIN = "800px 0px";

function supportsIntersectionObserver(): boolean {
  return typeof window !== "undefined" && typeof window.IntersectionObserver === "function";
}

export function LazyRichBlock({
  reservedHeightPx,
  children,
}: {
  /** Expected height of the mounted block; reserved before and after mount. */
  reservedHeightPx: number;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  // ALWAYS false on the first render, on both server and client.
  //
  // Deriving this from feature detection (`typeof window`, IntersectionObserver
  // presence) would make the first frame environment-dependent: the server, with
  // no `window`, would render the full Mermaid/HTML subtree while the browser's
  // hydration pass renders a placeholder — a markup mismatch, and an SSR that
  // silently bypasses the lazy gate it is supposed to honour. `"use client"`
  // does not opt a component out of Next's server render, so the only safe
  // initial state is the one both environments can agree on.
  //
  // Everything environment-specific happens in the effect below, which never
  // runs on the server.
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    if (mounted) return;

    // No IntersectionObserver (jsdom, older webviews): fall back to mounting
    // eagerly. This runs in an effect rather than in the initial state so the
    // first committed frame still matches the server's.
    if (!supportsIntersectionObserver()) {
      setMounted(true);
      return;
    }

    const el = ref.current;
    if (!el) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          // Latch: stop observing so the block never unmounts on scroll-away.
          setMounted(true);
          observer.disconnect();
        }
      },
      { rootMargin: NEAR_VIEWPORT_ROOT_MARGIN },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [mounted]);

  return (
    <div
      ref={ref}
      data-rich-block-shell=""
      data-mounted={mounted ? "" : undefined}
      // The reservation persists after mount so the shell never shrinks back
      // and re-triggers a measurement pass. Real diagrams and the fixed-height
      // HTML preview normally exceed it, so it rarely adds visible space.
      style={{ minHeight: reservedHeightPx }}
    >
      {mounted ? children : <RichBlockPlaceholder />}
    </div>
  );
}

/**
 * Pre-mount placeholder. Deliberately inert and unlabelled-as-loading: it fills
 * reserved space rather than announcing work, because nothing is loading yet —
 * the block simply has not been scrolled near.
 */
function RichBlockPlaceholder() {
  return (
    <div
      className="my-3 h-full w-full rounded-md border border-border/50 bg-muted/20"
      aria-hidden="true"
    />
  );
}
