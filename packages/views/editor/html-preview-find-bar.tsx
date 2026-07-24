"use client";

/**
 * HtmlPreviewFindBar — an in-app find bar for sandboxed HTML preview iframes
 * (#5259). Native Ctrl+F cannot search into an opaque-origin sandbox iframe, so
 * this bar drives the injected find shim (see utils/iframe-find.ts) over
 * postMessage instead: typing searches, Enter / arrows step through matches, and
 * the shim reports back a `current/total` count the caller feeds in via `result`.
 *
 * The bar owns only the query/UI; the actual find + scroll happens inside the
 * iframe's own document. Keep it presentational so both the full-page viewer and
 * the modal can reuse it.
 */

import { useEffect, useRef, type RefObject } from "react";
import { ChevronDown, ChevronUp, X } from "lucide-react";
import { useT } from "../i18n";
import { FIND_CMD } from "./utils/iframe-find";

export interface FindResult {
  found: boolean;
  total: number;
  current: number;
}

interface HtmlPreviewFindBarProps {
  /** The sandboxed preview iframe to drive over postMessage. */
  iframeRef: RefObject<HTMLIFrameElement | null>;
  /** Latest count reported by the iframe shim, or null before the first search. */
  result: FindResult | null;
  /** Current query (controlled by the caller so it survives bar remounts). */
  query: string;
  onQueryChange: (query: string) => void;
  onClose: () => void;
}

export function HtmlPreviewFindBar({
  iframeRef,
  result,
  query,
  onQueryChange,
  onClose,
}: HtmlPreviewFindBarProps) {
  const { t } = useT("editor");
  const inputRef = useRef<HTMLInputElement>(null);

  // Focus the input whenever the bar mounts so Ctrl+F feels native.
  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);

  const send = (action: "search" | "next" | "prev" | "clear", q: string) => {
    iframeRef.current?.contentWindow?.postMessage(
      { source: FIND_CMD, action, query: q, caseSensitive: false },
      "*",
    );
  };

  const handleChange = (value: string) => {
    onQueryChange(value);
    send("search", value);
  };

  const step = (dir: "next" | "prev") => {
    if (query) send(dir, query);
  };

  const close = () => {
    send("clear", "");
    onClose();
  };

  const hasQuery = query.length > 0;
  const noResults = hasQuery && result != null && result.total === 0;
  const count =
    hasQuery && result != null && result.total > 0
      ? t(($) => $.attachment.find_count, {
          current: String(result.current),
          total: String(result.total),
        })
      : noResults
      ? t(($) => $.attachment.find_no_results)
      : "";

  return (
    <div
      className="absolute right-3 top-3 z-10 flex items-center gap-1 rounded-md border border-border bg-background/95 px-2 py-1 shadow-md backdrop-blur"
      role="search"
    >
      <input
        ref={inputRef}
        type="text"
        value={query}
        onChange={(e) => handleChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            step(e.shiftKey ? "prev" : "next");
          } else if (e.key === "Escape") {
            e.preventDefault();
            close();
          }
        }}
        placeholder={t(($) => $.attachment.find_placeholder)}
        aria-label={t(($) => $.attachment.find_placeholder)}
        className="w-40 bg-transparent text-sm text-foreground outline-none placeholder:text-muted-foreground"
      />
      <span className="min-w-[3rem] shrink-0 text-right text-xs tabular-nums text-muted-foreground">
        {count}
      </span>
      <button
        type="button"
        className="rounded p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-40"
        title={t(($) => $.attachment.find_prev)}
        aria-label={t(($) => $.attachment.find_prev)}
        disabled={!hasQuery}
        onClick={() => step("prev")}
      >
        <ChevronUp className="size-4" />
      </button>
      <button
        type="button"
        className="rounded p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground disabled:opacity-40"
        title={t(($) => $.attachment.find_next)}
        aria-label={t(($) => $.attachment.find_next)}
        disabled={!hasQuery}
        onClick={() => step("next")}
      >
        <ChevronDown className="size-4" />
      </button>
      <button
        type="button"
        className="rounded p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
        title={t(($) => $.attachment.find_close)}
        aria-label={t(($) => $.attachment.find_close)}
        onClick={close}
      >
        <X className="size-4" />
      </button>
    </div>
  );
}
