"use client";

import * as React from "react";
import { X, ChevronUp, ChevronDown } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  countMatches,
  replaceAll,
  replaceFirst,
} from "@multica/cerebro-artifacts/core";

/**
 * Inline find & replace bar for an open note/document. It works directly on the
 * markdown body (both surfaces inline-edit, so there is no separate edit field):
 * the parent passes the current body, this bar computes matches and hands back
 * the replaced body, which the parent writes into the inline editor + autosaves.
 *
 * Shared by the Documents view (DocumentViewPage) and the Notes view
 * (NoteEditor) so both surfaces get identical find & replace (FIR-1647).
 *
 * FIR-2145: added Previous/Next navigation and onFindStateChange callback so
 * the parent (NoteEditor) can drive the find-highlight plugin.
 */
export function FindReplaceBar({
  body,
  onReplaceAll,
  onReplaceFirst,
  onClose,
  onFindStateChange,
}: {
  body: string;
  onReplaceAll: (newBody: string) => void;
  onReplaceFirst: (newBody: string) => void;
  onClose: () => void;
  /** Called whenever the search query or active match index changes. */
  onFindStateChange?: (
    query: string,
    activeIndex: number,
    total: number,
  ) => void;
}) {
  const [find, setFind] = React.useState("");
  const [replacement, setReplacement] = React.useState("");
  const [activeIndex, setActiveIndex] = React.useState(-1);

  const matches = React.useMemo(() => countMatches(body, find), [body, find]);

  // Reset active index whenever the query or match count changes.
  React.useEffect(() => {
    setActiveIndex(matches > 0 ? 0 : -1);
  }, [find, matches]);

  // Notify parent of state changes so it can drive editor highlighting.
  React.useEffect(() => {
    onFindStateChange?.(find, activeIndex, matches);
  }, [find, activeIndex, matches, onFindStateChange]);

  function goNext() {
    if (matches === 0) return;
    setActiveIndex((i) => (i + 1) % matches);
  }

  function goPrev() {
    if (matches === 0) return;
    setActiveIndex((i) => (i - 1 + matches) % matches);
  }

  const doReplaceAll = () => {
    if (!find) return;
    const { body: next, count } = replaceAll(body, find, replacement);
    if (count === 0) {
      toast.info("No matches to replace.");
      return;
    }
    onReplaceAll(next);
    toast.success(`Replaced ${count} ${count === 1 ? "match" : "matches"}.`);
  };

  const doReplaceFirst = () => {
    if (!find) return;
    const { body: next, replaced } = replaceFirst(body, find, replacement);
    if (!replaced) {
      toast.info("No matches to replace.");
      return;
    }
    onReplaceFirst(next);
  };

  return (
    <div className="mb-3 flex flex-wrap items-center gap-2 rounded-md border bg-muted/30 px-3 py-2">
      <div className="flex items-center gap-1.5">
        <Input
          autoFocus
          value={find}
          onChange={(e) => setFind(e.target.value)}
          placeholder="Search…"
          className="h-8 w-40"
          onKeyDown={(e) => {
            if (e.key === "Escape") onClose();
            if (e.key === "Enter") {
              e.shiftKey ? goPrev() : goNext();
            }
          }}
        />
        <span className="min-w-14 text-xs text-muted-foreground">
          {find
            ? matches > 0
              ? `${activeIndex + 1}/${matches}`
              : "0 found"
            : ""}
        </span>
      </div>
      {/* Previous / Next navigation (FIR-2145). */}
      <div className="flex items-center gap-0.5">
        <Button
          variant="outline"
          size="sm"
          className="h-8 px-2"
          onClick={goPrev}
          disabled={matches === 0}
          title="Previous match (Shift+Enter)"
          aria-label="Previous match"
        >
          <ChevronUp className="size-3.5" />
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="h-8 px-2"
          onClick={goNext}
          disabled={matches === 0}
          title="Next match (Enter)"
          aria-label="Next match"
        >
          <ChevronDown className="size-3.5" />
        </Button>
      </div>
      <Input
        value={replacement}
        onChange={(e) => setReplacement(e.target.value)}
        placeholder="Replace with…"
        className="h-8 w-40"
        onKeyDown={(e) => {
          if (e.key === "Escape") onClose();
        }}
      />
      <Button
        variant="outline"
        size="sm"
        onClick={doReplaceFirst}
        disabled={matches === 0}
      >
        Replace
      </Button>
      <Button
        variant="outline"
        size="sm"
        onClick={doReplaceAll}
        disabled={matches === 0}
      >
        Replace all
      </Button>
      <Button
        variant="ghost"
        size="sm"
        className="ml-auto"
        title="Close"
        onClick={onClose}
      >
        <X className="size-4" />
      </Button>
    </div>
  );
}
