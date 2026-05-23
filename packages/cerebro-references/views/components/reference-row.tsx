"use client";

import { ExternalLink, Trash2 } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  getObjectRenderer,
  type IssueReference,
} from "@multica/cerebro-references/core";
import { ObjectIcon } from "./object-icon";
import { ReferenceBadge } from "./reference-badge";

// A single reference line: object glyph, resolved title (links out when the
// renderer can produce a URL), state badge, and a delete affordance.
//
// Everything user-visible flows through the core renderer registry
// (getObjectRenderer), so an unknown object kind still renders a sensible
// row via the fallback renderer — no per-type frontend code required.
export function ReferenceRow({
  reference,
  onDelete,
  deleting = false,
  className,
}: {
  reference: IssueReference;
  onDelete?: (reference: IssueReference) => void;
  deleting?: boolean;
  className?: string;
}) {
  const renderer = getObjectRenderer(reference.object);
  const title = renderer.formatTitle(reference);
  const url = renderer.resolveUrl(reference);

  return (
    <div
      data-testid="reference-row"
      data-object={reference.object}
      className={cn(
        "group flex items-center gap-2 rounded-md border border-border bg-muted/50 px-2.5 py-1 transition-colors hover:bg-muted",
        className,
      )}
    >
      <ObjectIcon object={reference.object} className="size-4 shrink-0 text-muted-foreground" />
      {url ? (
        <a
          href={url}
          target="_blank"
          rel="noreferrer"
          className="min-w-0 flex-1 truncate text-left text-sm hover:underline"
          title={title}
        >
          {title}
        </a>
      ) : (
        <span className="min-w-0 flex-1 truncate text-left text-sm" title={title}>
          {title}
        </span>
      )}
      <ReferenceBadge reference={reference} />
      {url && (
        <a
          href={url}
          target="_blank"
          rel="noreferrer"
          aria-label="Open reference"
          title="Open reference"
          className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
        >
          <ExternalLink className="size-3.5" />
        </a>
      )}
      {onDelete && (
        <button
          type="button"
          aria-label="Remove reference"
          title="Remove reference"
          disabled={deleting}
          className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-destructive disabled:opacity-50"
          onClick={() => onDelete(reference)}
        >
          <Trash2 className="size-3.5" />
        </button>
      )}
    </div>
  );
}
