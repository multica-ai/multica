"use client";

/**
 * AttachmentChip — the unified, fixed-size attachment card used across every
 * cerebro surface (FIR-2034).
 *
 * Two shapes, one consistent height (56px) so a row of mixed attachments
 * always lines up:
 *   - image → a real thumbnail of the picture (NOT a file box).
 *   - document → a 212×56 card with a type-specific colour + icon, the
 *     filename, and a type label (PDF, Excel, Word, …).
 *
 * This is pure presentation: it owns no preview/download/remove logic and
 * reads no feature flag. Callers decide the kind (pass `thumbnailSrc` for an
 * image), wire the actions, and gate the whole thing behind
 * `cerebro_attachment_chips`. Living in `@multica/cerebro-ui` (which depends
 * only on `@multica/ui`) lets both the posted-state list
 * (`@multica/cerebro-attachments`) and the upstream editor card
 * (`@multica/views`) render the identical chip without a circular dependency.
 */

import {
  File,
  FileArchive,
  FileCode,
  FileSpreadsheet,
  FileText,
  FileType,
  Loader2,
  Presentation,
  X,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";

// Fixed card geometry — every document card is exactly this size so a row of
// attachments stays tidy regardless of filename length. The image thumbnail
// shares the height but keeps its own (capped) width.
export const ATTACHMENT_CHIP_WIDTH = 212;
export const ATTACHMENT_CHIP_HEIGHT = 56;

export type AttachmentDocType =
  | "pdf"
  | "excel"
  | "word"
  | "powerpoint"
  | "archive"
  | "code"
  | "other";

interface DocTypeStyle {
  label: string;
  Icon: LucideIcon;
  /** Icon colour. */
  iconClass: string;
  /** Soft tinted background behind the icon tile. */
  tileClass: string;
}

// Type palette. Colours are intentionally literal (not semantic tokens): a
// file's type colour should read the same in every theme, the way Slack /
// Linear colour their file pills. Each pairs a light + dark variant so the
// tint stays legible on both backgrounds.
const DOC_TYPE_STYLES: Record<AttachmentDocType, DocTypeStyle> = {
  pdf: {
    label: "PDF",
    Icon: FileText,
    iconClass: "text-red-600 dark:text-red-400",
    tileClass: "bg-red-100 dark:bg-red-500/15",
  },
  excel: {
    label: "Excel",
    Icon: FileSpreadsheet,
    iconClass: "text-green-600 dark:text-green-400",
    tileClass: "bg-green-100 dark:bg-green-500/15",
  },
  word: {
    label: "Word",
    Icon: FileType,
    iconClass: "text-blue-600 dark:text-blue-400",
    tileClass: "bg-blue-100 dark:bg-blue-500/15",
  },
  powerpoint: {
    label: "PowerPoint",
    Icon: Presentation,
    iconClass: "text-orange-600 dark:text-orange-400",
    tileClass: "bg-orange-100 dark:bg-orange-500/15",
  },
  archive: {
    label: "Archive",
    Icon: FileArchive,
    iconClass: "text-amber-600 dark:text-amber-400",
    tileClass: "bg-amber-100 dark:bg-amber-500/15",
  },
  code: {
    label: "Code",
    Icon: FileCode,
    iconClass: "text-violet-600 dark:text-violet-400",
    tileClass: "bg-violet-100 dark:bg-violet-500/15",
  },
  other: {
    label: "File",
    Icon: File,
    iconClass: "text-muted-foreground",
    tileClass: "bg-muted",
  },
};

const EXT_TO_DOC_TYPE: Record<string, AttachmentDocType> = {
  pdf: "pdf",
  xlsx: "excel",
  xls: "excel",
  csv: "excel",
  tsv: "excel",
  docx: "word",
  doc: "word",
  rtf: "word",
  pptx: "powerpoint",
  ppt: "powerpoint",
  zip: "archive",
  gz: "archive",
  tar: "archive",
  rar: "archive",
  "7z": "archive",
  ts: "code",
  tsx: "code",
  js: "code",
  jsx: "code",
  json: "code",
  sql: "code",
  py: "code",
  go: "code",
  rs: "code",
  rb: "code",
  java: "code",
  sh: "code",
  yaml: "code",
  yml: "code",
  html: "code",
  css: "code",
  xml: "code",
};

function extensionOf(filename: string): string {
  const base = (filename.toLowerCase().split(/[\\/]/).pop() ?? "").trim();
  const dot = base.lastIndexOf(".");
  if (dot <= 0) return "";
  return base.slice(dot + 1);
}

/** Map a filename to its document type for the chip palette. */
export function attachmentDocType(filename: string): AttachmentDocType {
  return EXT_TO_DOC_TYPE[extensionOf(filename)] ?? "other";
}

export interface AttachmentChipProps {
  filename: string;
  /** When set, the chip renders this image as a thumbnail instead of a card. */
  thumbnailSrc?: string;
  /** Primary action (open preview / download). Whole chip is the trigger. */
  onActivate?: () => void;
  /** Tooltip + aria hint for the primary action. */
  activateLabel?: string;
  /** Optional remove handler — shows an × on hover/focus when provided. */
  onRemove?: () => void;
  /** Renders a spinner + dimmed state while an upload is in flight. */
  uploading?: boolean;
  className?: string;
}

export function AttachmentChip({
  filename,
  thumbnailSrc,
  onActivate,
  activateLabel,
  onRemove,
  uploading,
  className,
}: AttachmentChipProps) {
  const isImage = !!thumbnailSrc;
  const removeButton = onRemove ? (
    <button
      type="button"
      aria-label="Remove attachment"
      title="Remove attachment"
      className={cn(
        "absolute -right-1.5 -top-1.5 z-10 rounded-full border border-border bg-background p-0.5 text-muted-foreground opacity-0 shadow-sm transition-opacity",
        "hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100 group-focus-within:opacity-100",
      )}
      // Stop the editor / drag handlers from hijacking the click in composer
      // surfaces (mirrors the upstream AttachmentCard guards).
      onMouseDown={(e) => {
        e.preventDefault();
        e.stopPropagation();
      }}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        onRemove();
      }}
    >
      <X className="size-3" />
    </button>
  ) : null;

  if (isImage) {
    return (
      <span className={cn("group relative inline-flex", className)}>
        <button
          type="button"
          onClick={onActivate}
          title={activateLabel}
          aria-label={activateLabel ?? filename}
          className={cn(
            "block h-14 w-auto min-w-14 max-w-[160px] overflow-hidden rounded-md border border-border bg-muted/50 transition-colors",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            uploading && "opacity-60",
          )}
          onMouseDown={(e) => e.stopPropagation()}
        >
          <img
            src={thumbnailSrc}
            alt={filename}
            // Force the chip's own 56px height/width: the upstream readonly
            // editor ships a generic `.rich-text-editor img { height:auto; margin }`
            // rule (specificity 0-1-1) that otherwise defeats `h-14` when a
            // posted comment body renders this chip, stretching it to ~100px.
            className="!my-0 !h-14 !w-auto max-w-[160px] object-cover"
            draggable={false}
          />
        </button>
        {removeButton}
      </span>
    );
  }

  const { label, Icon, iconClass, tileClass } = DOC_TYPE_STYLES[
    attachmentDocType(filename)
  ];
  return (
    <span className={cn("group relative inline-flex", className)}>
      <button
        type="button"
        onClick={onActivate}
        title={activateLabel ?? filename}
        aria-label={activateLabel ? `${activateLabel}: ${filename}` : filename}
        className={cn(
          "flex h-14 w-[212px] items-center gap-2.5 rounded-md border border-border bg-muted/50 px-2.5 text-left transition-colors",
          "hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        )}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <span
          className={cn(
            "flex size-9 shrink-0 items-center justify-center rounded-md",
            tileClass,
          )}
        >
          {uploading ? (
            <Loader2 className="size-4 animate-spin text-muted-foreground" />
          ) : (
            <Icon className={cn("size-5", iconClass)} />
          )}
        </span>
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-sm leading-tight">{filename}</span>
          <span className="text-xs leading-tight text-muted-foreground">
            {label}
          </span>
        </span>
      </button>
      {removeButton}
    </span>
  );
}
