"use client";

import * as React from "react";
import { X } from "lucide-react";
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
 */
export function FindReplaceBar({
  body,
  onReplaceAll,
  onReplaceFirst,
  onClose,
}: {
  body: string;
  onReplaceAll: (newBody: string) => void;
  onReplaceFirst: (newBody: string) => void;
  onClose: () => void;
}) {
  const [find, setFind] = React.useState("");
  const [replacement, setReplacement] = React.useState("");
  const matches = React.useMemo(() => countMatches(body, find), [body, find]);

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
          }}
        />
        <span className="min-w-14 text-xs text-muted-foreground">
          {find ? `${matches} found` : ""}
        </span>
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
