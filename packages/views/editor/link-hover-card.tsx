"use client";

/**
 * LinkHoverCard — floating card shown on link hover.
 *
 * Displays the URL with Copy and Open actions. Portaled to body
 * with position:fixed to escape overflow:hidden containers.
 * Shows after 300ms hover delay, hides after 150ms mouse-out
 * (cancelled if mouse enters the card).
 */

import { useState, useEffect, useCallback, useRef } from "react";
import { createPortal } from "react-dom";
import { computePosition, offset, flip, shift } from "@floating-ui/dom";
import { ExternalLink, Copy } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { openLink, isMentionHref } from "./utils/link-handler";

function truncateUrl(url: string, max = 48): string {
  if (url.length <= max) return url;
  try {
    const u = new URL(url);
    const origin = u.origin;
    const rest = url.slice(origin.length);
    if (rest.length <= 10) return url;
    return `${origin}${rest.slice(0, max - origin.length - 1)}…`;
  } catch {
    return `${url.slice(0, max - 1)}…`;
  }
}

// ---------------------------------------------------------------------------
// Hook — manages hover state with enter/leave delays
// ---------------------------------------------------------------------------

const SHOW_DELAY = 300;
const HIDE_DELAY = 150;

interface HoverState {
  visible: boolean;
  href: string;
  anchorEl: HTMLAnchorElement | null;
}

function useLinkHover(containerRef: React.RefObject<HTMLElement | null>, disabled?: boolean) {
  const [state, setState] = useState<HoverState>({ visible: false, href: "", anchorEl: null });
  const showTimer = useRef(0);
  const hideTimer = useRef(0);
  const cardRef = useRef<HTMLDivElement>(null);

  const clearTimers = useCallback(() => {
    clearTimeout(showTimer.current);
    clearTimeout(hideTimer.current);
  }, []);

  // Container mouse events — detect <a> hover
  useEffect(() => {
    const container = containerRef.current;
    if (!container || disabled) return;

    const onMouseOver = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      const link = target.closest("a") as HTMLAnchorElement | null;
      if (!link) return;
      const href = link.getAttribute("href");
      if (!href || isMentionHref(href)) return;
      // Issue mention cards render as <a class="issue-mention"> — they
      // display their own rich info, a URL hover card is redundant.
      if (link.classList.contains("issue-mention")) return;

      clearTimeout(hideTimer.current);
      showTimer.current = window.setTimeout(() => {
        setState({ visible: true, href, anchorEl: link });
      }, SHOW_DELAY);
    };

    const onMouseOut = (e: MouseEvent) => {
      const related = e.relatedTarget as HTMLElement | null;
      // Don't hide if mouse moved to the hover card
      if (related && cardRef.current?.contains(related)) return;
      // Don't hide if mouse moved to another part of the same link
      const link = (e.target as HTMLElement).closest("a");
      if (link && link.contains(related)) return;

      clearTimeout(showTimer.current);
      hideTimer.current = window.setTimeout(() => {
        setState((s) => ({ ...s, visible: false }));
      }, HIDE_DELAY);
    };

    container.addEventListener("mouseover", onMouseOver);
    container.addEventListener("mouseout", onMouseOut);
    return () => {
      container.removeEventListener("mouseover", onMouseOver);
      container.removeEventListener("mouseout", onMouseOut);
      clearTimers();
    };
  }, [containerRef, disabled, clearTimers]);

  // Card mouse events — keep visible while hovering the card
  const onCardEnter = useCallback(() => {
    clearTimeout(hideTimer.current);
  }, []);

  const onCardLeave = useCallback(() => {
    hideTimer.current = window.setTimeout(() => {
      setState((s) => ({ ...s, visible: false }));
    }, HIDE_DELAY);
  }, []);

  return { ...state, cardRef, onCardEnter, onCardLeave };
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

function LinkHoverCard({
  visible,
  href,
  anchorEl,
  cardRef,
  onCardEnter,
  onCardLeave,
}: {
  visible: boolean;
  href: string;
  anchorEl: HTMLAnchorElement | null;
  cardRef: React.RefObject<HTMLDivElement | null>;
  onCardEnter: () => void;
  onCardLeave: () => void;
}) {
  const [pos, setPos] = useState({ top: 0, left: 0 });
  const [positioned, setPositioned] = useState(false);

  // Position the card when the portal div is mounted (ref callback).
  // Using useEffect would race with portal rendering — the div might
  // not be in the DOM yet when the effect runs.
  const setCardRef = useCallback(
    (node: HTMLDivElement | null) => {
      (cardRef as React.MutableRefObject<HTMLDivElement | null>).current = node;
      if (!node || !anchorEl) {
        setPositioned(false);
        return;
      }
      computePosition(anchorEl, node, {
        placement: "bottom-start",
        strategy: "fixed",
        middleware: [offset(4), flip(), shift({ padding: 8 })],
      }).then(({ x, y }) => {
        setPos({ top: y, left: x });
        setPositioned(true);
      });
    },
    [anchorEl, cardRef],
  );

  // Reset positioned when hidden
  useEffect(() => {
    if (!visible) setPositioned(false);
  }, [visible]);

  if (!visible || !anchorEl) return null;

  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation();
    e.preventDefault();
    try {
      await navigator.clipboard.writeText(href);
      toast.success("Link copied");
    } catch {
      toast.error("Failed to copy");
    }
  };

  const handleOpen = (e: React.MouseEvent) => {
    e.stopPropagation();
    e.preventDefault();
    openLink(href);
  };

  return createPortal(
    <div
      ref={setCardRef}
      className="link-hover-card"
      style={{
        position: "fixed",
        top: pos.top,
        left: pos.left,
        zIndex: 50,
        display: positioned ? undefined : "none",
      }}
      onMouseEnter={onCardEnter}
      onMouseLeave={onCardLeave}
    >
      <span
        className="min-w-0 flex-1 truncate text-xs text-muted-foreground px-1"
        title={href}
      >
        {truncateUrl(href)}
      </span>
      <Button
        size="icon-xs"
        variant="ghost"
        className="text-muted-foreground"
        onClick={handleCopy}
        title="Copy link"
      >
        <Copy className="size-3.5" />
      </Button>
      <Button
        size="icon-xs"
        variant="ghost"
        className="text-muted-foreground"
        onClick={handleOpen}
        title="Open link"
      >
        <ExternalLink className="size-3.5" />
      </Button>
    </div>,
    document.body,
  );
}

export { useLinkHover, LinkHoverCard };
