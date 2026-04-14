"use client";

/**
 * Link preview for ReadonlyContent (react-markdown).
 *
 * Shows a floating card (Copy + Open) when clicking a link in readonly
 * content. Uses @floating-ui/react-dom portaled to body to escape
 * overflow:hidden containers.
 */

import {
  useState,
  useEffect,
  useCallback,
  useRef,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { useFloating, offset, flip, shift } from "@floating-ui/react-dom";
import { getOverflowAncestors } from "@floating-ui/dom";
import { ExternalLink, Copy } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function openLink(href: string) {
  if (href.startsWith("/")) {
    window.dispatchEvent(
      new CustomEvent("multica:navigate", { detail: { path: href } }),
    );
  } else {
    window.open(href, "_blank", "noopener,noreferrer");
  }
}

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
// LinkPreviewCard — pure UI
// ---------------------------------------------------------------------------

function LinkPreviewCard({
  href,
  onMouseDown,
}: {
  href: string;
  onMouseDown?: (e: React.MouseEvent) => void;
}) {
  const handleCopy = useCallback(
    async (e: React.MouseEvent) => {
      e.stopPropagation();
      try {
        await navigator.clipboard.writeText(href);
        toast.success("Link copied");
      } catch {
        toast.error("Failed to copy");
      }
    },
    [href],
  );

  const handleOpen = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      openLink(href);
    },
    [href],
  );

  return (
    <div className="link-preview-card" onMouseDown={onMouseDown}>
      <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground px-1" title={href}>
        {truncateUrl(href)}
      </span>
      <Button size="icon-xs" variant="ghost" className="text-muted-foreground" onClick={handleCopy} title="Copy link">
        <Copy className="size-3.5" />
      </Button>
      <Button size="icon-xs" variant="ghost" className="text-muted-foreground" onClick={handleOpen} title="Open link">
        <ExternalLink className="size-3.5" />
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Shared hooks
// ---------------------------------------------------------------------------

function useCloseOnOutsideClick(active: boolean, close: () => void) {
  useEffect(() => {
    if (!active) return;
    const handle = (e: MouseEvent) => {
      if ((e.target as HTMLElement).closest(".link-preview-card")) return;
      close();
    };
    const t = setTimeout(() => document.addEventListener("mousedown", handle), 0);
    return () => { clearTimeout(t); document.removeEventListener("mousedown", handle); };
  }, [active, close]);
}

function useCloseOnEscape(active: boolean, close: () => void) {
  useEffect(() => {
    if (!active) return;
    const handle = (e: KeyboardEvent) => { if (e.key === "Escape") close(); };
    document.addEventListener("keydown", handle);
    return () => document.removeEventListener("keydown", handle);
  }, [active, close]);
}

// ---------------------------------------------------------------------------
// ReadonlyLinkWrapper — for ReadonlyContent (react-markdown)
// ---------------------------------------------------------------------------

function ReadonlyLinkWrapper({
  href,
  children,
}: {
  href: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const anchorRef = useRef<HTMLAnchorElement>(null);

  const close = useCallback(() => setOpen(false), []);

  const { refs, floatingStyles } = useFloating({
    strategy: "fixed",
    placement: "bottom-start",
    middleware: [offset(4), flip(), shift({ padding: 8 })],
    elements: { reference: anchorRef.current },
    open,
  });

  const toggle = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      setOpen((v) => !v);
    },
    [],
  );

  // Close on any ancestor scroll
  useEffect(() => {
    if (!open || !anchorRef.current) return;
    const hide = () => setOpen(false);
    const ancestors = getOverflowAncestors(anchorRef.current);
    ancestors.forEach((el) => el.addEventListener("scroll", hide, { passive: true }));
    return () => { ancestors.forEach((el) => el.removeEventListener("scroll", hide)); };
  }, [open]);

  useCloseOnOutsideClick(open, close);
  useCloseOnEscape(open, close);

  return (
    <>
      <a ref={anchorRef} href={href} onClick={toggle} role="button" aria-expanded={open}>
        {children}
      </a>
      {open &&
        createPortal(
          <div ref={refs.setFloating} style={{ ...floatingStyles, zIndex: 50 }}>
            <LinkPreviewCard href={href} />
          </div>,
          document.body,
        )}
    </>
  );
}

export { ReadonlyLinkWrapper, openLink };
