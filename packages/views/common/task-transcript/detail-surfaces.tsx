"use client";

import { useMemo, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { redactSecrets } from "./redact";
import type { TraceDiffLine, TracePatchFile } from "./trace-event-presenter";
import { highlightBlock, highlightToLines, languageForPath } from "./diff-highlight";
import { useT } from "../../i18n";
import "../../editor/styles/code.css";
import "./task-transcript.css";

// Bodies for one step's input and result. Extracted from the transcript dialog
// so the dialog file is about the run, and these are about rendering a payload.
// ─── Tool detail surface ────────────────────────────────────────────────────

/**
 * Long content fades out behind a "show all" affordance instead of trapping a
 * nested scrollbar inside the virtualized list.
 */
type HighlightedSides = Record<"add" | "remove" | "context", string[] | null> | null;

/**
 * Diff rows. Highlighting is looked up per side, since each side was
 * highlighted as its own block; a side that failed to highlight falls back to
 * plain text for that side only.
 */
export function renderDiffRows(lines: TraceDiffLine[], highlighted: HighlightedSides) {
  const cursor: Record<"add" | "remove" | "context", number> = { add: 0, remove: 0, context: 0 };

  return lines.map((line, index) => {
    if (line.kind === "gap") {
      return (
        // Transcript events are immutable once persisted, so index is stable.
        <div key={index} className="select-none text-faint-foreground" aria-hidden>
          {"  ⋯"}
        </div>
      );
    }

    const kind = line.kind;
    const html = highlighted?.[kind]?.[cursor[kind]];
    cursor[kind] += 1;

    return (
      <div
        key={index}
        className={cn(
          "-mx-1 px-1",
          kind === "add" && "bg-success/10",
          kind === "remove" && "bg-destructive/10",
          // Only tint the gutter for changed rows; the code itself keeps its
          // syntax colours so a diff reads like the file it came from.
          !html && kind === "add" && "text-success",
          !html && kind === "remove" && "text-destructive",
          !html && kind === "context" && "text-muted-foreground",
        )}
      >
        <span
          aria-hidden
          className={cn(
            "select-none",
            kind === "add" && "text-success",
            kind === "remove" && "text-destructive",
          )}
        >
          {kind === "add" ? "+" : kind === "remove" ? "-" : " "}
        </span>{" "}
        {html === undefined ? (
          redactSecrets(line.text)
        ) : (
          // lowlight output only: text is escaped by the hast serializer and
          // the sole elements are its own `hljs-*` spans.
          <span className="hljs" dangerouslySetInnerHTML={{ __html: html }} />
        )}
      </div>
    );
  });
}

/**
 * A whole-file write has no before side, so it reads as plain content with a
 * line count rather than as an all-additions diff — the `+` gutter would carry
 * no information here.
 */
export function FileWriteSurface({
  text,
  lineCount,
  path,
}: {
  text: string;
  lineCount: number;
  path: string;
}) {
  return (
    <div>
      <div className="px-3 pt-2 font-mono text-micro text-success">+{lineCount}</div>
      <ToolDetailSurface text={redactSecrets(text)} language={languageForPath(path)} />
    </div>
  );
}

/**
 * A multi-file patch: one section per file, each reusing the single-file
 * surfaces. Codex's `patch_apply` routinely touches several files in one call,
 * so collapsing them into a single body would lose which change belongs where.
 */
export function PatchDetailSurface({
  files,
  truncated,
}: {
  files: TracePatchFile[];
  truncated: boolean;
}) {
  const { t } = useT("agents");
  return (
    <div className="divide-y divide-border/40">
      {files.map((file, index) => (
        // Transcript events are immutable once persisted, so index is stable.
        <div key={`${file.path}:${index}`}>
          <div className="flex items-center gap-2 px-3 pt-2 font-mono text-micro">
            {file.changeKind && (
              <span
                className={cn(
                  "shrink-0 uppercase",
                  file.changeKind === "add" && "text-success",
                  file.changeKind === "delete" && "text-destructive",
                  file.changeKind === "update" && "text-muted-foreground",
                )}
              >
                {file.changeKind}
              </span>
            )}
            <span className="truncate text-muted-foreground">{file.path}</span>
            {file.movePath && (
              <>
                <span className="shrink-0 text-faint-foreground" aria-hidden>
                  →
                </span>
                <span className="truncate text-muted-foreground">{file.movePath}</span>
              </>
            )}
          </div>
          {file.body.kind === "diff" ? (
            <DiffDetailSurface lines={file.body.lines} path={file.path} />
          ) : file.body.kind === "file" ? (
            <FileWriteSurface
              text={file.body.text}
              lineCount={file.body.lineCount}
              path={file.path}
            />
          ) : (
            <div className="px-3 pb-2 pt-1 font-mono text-micro text-muted-foreground">
              {file.truncated
                ? t(($) => $.transcript.patch_body_truncated)
                : t(($) => $.transcript.patch_no_content)}
            </div>
          )}
        </div>
      ))}
      {truncated && (
        <div className="px-3 py-2 font-mono text-micro text-muted-foreground">
          {t(($) => $.transcript.patch_truncated)}
        </div>
      )}
    </div>
  );
}

/**
 * A file change reads as a diff rather than two escaped string literals. Same
 * fade/"show all" shell as the text surface so both bodies behave alike inside
 * the virtualized list.
 */
export function DiffDetailSurface({ lines, path }: { lines: TraceDiffLine[]; path: string }) {
  const { t } = useT("agents");
  const [showAll, setShowAll] = useState(false);
  const isLong = lines.length > 14;
  const added = lines.filter((l) => l.kind === "add").length;
  const removed = lines.filter((l) => l.kind === "remove").length;

  // Each side is highlighted as one block so multi-line strings and comments
  // keep their grammar, then split back per line to sit in the diff gutter.
  // Runs only when the row is expanded, which is where this component mounts.
  const highlighted = useMemo(() => {
    const language = languageForPath(path);
    if (!language) return null;
    const sides: Record<"add" | "remove" | "context", string[] | null> = {
      add: null,
      remove: null,
      context: null,
    };
    for (const kind of ["add", "remove", "context"] as const) {
      const side = lines.filter((l) => l.kind === kind);
      if (side.length > 0) {
        sides[kind] = highlightToLines(side.map((l) => l.text).join("\n"), language);
      }
    }
    return sides;
  }, [lines, path]);

  return (
    <div className="relative">
      <div className="flex items-center gap-2 px-3 pt-2 font-mono text-micro text-muted-foreground">
        {added > 0 && <span className="text-success">+{added}</span>}
        {removed > 0 && <span className="text-destructive">-{removed}</span>}
      </div>
      <pre
        className={cn(
          "transcript-code px-3 pb-3 pt-1 font-mono text-micro whitespace-pre-wrap break-all",
          isLong && !showAll && "max-h-52 overflow-hidden",
        )}
      >
        {renderDiffRows(lines, highlighted)}
      </pre>
      {isLong && !showAll && (
        <div className="absolute inset-x-0 bottom-0 flex h-12 items-end justify-center rounded-b-md bg-gradient-to-b from-transparent to-background">
          <button
            type="button"
            onClick={() => setShowAll(true)}
            // Opaque: the gradient alone does not clear the clipped line, so a
            // transparent label lands on top of it and both become unreadable.
            className="mb-1.5 rounded border bg-background px-2 py-0.5 text-micro text-muted-foreground shadow-sm transition-colors hover:bg-accent hover:text-foreground"
          >
            {t(($) => $.transcript.show_all)}
          </button>
        </div>
      )}
    </div>
  );
}

export function ToolDetailSurface({ text, language }: { text: string; language?: string }) {
  const { t } = useT("agents");
  const [showAll, setShowAll] = useState(false);
  const isLong = text.length > 1600 || text.split("\n").length > 14;
  // Only file bodies carry a language; command output and JSON stay plain.
  const html = useMemo(() => (language ? highlightBlock(text, language) : null), [text, language]);

  return (
    <div className="relative">
      <pre
        className={cn(
          "transcript-code p-3 font-mono text-micro text-muted-foreground whitespace-pre-wrap break-all",
          isLong && !showAll && "max-h-52 overflow-hidden",
        )}
      >
        {html === null ? (
          text
        ) : (
          // lowlight output only: the hast serializer escapes text and the sole
          // elements are its own `hljs-*` spans.
          <code className="hljs" dangerouslySetInnerHTML={{ __html: html }} />
        )}
      </pre>
      {isLong && !showAll && (
        <div className="absolute inset-x-0 bottom-0 flex h-12 items-end justify-center rounded-b-md bg-gradient-to-b from-transparent to-background">
          <button
            type="button"
            onClick={() => setShowAll(true)}
            // Opaque: the gradient alone does not clear the clipped line, so a
            // transparent label lands on top of it and both become unreadable.
            className="mb-1.5 rounded border bg-background px-2 py-0.5 text-micro text-muted-foreground shadow-sm transition-colors hover:bg-accent hover:text-foreground"
          >
            {t(($) => $.transcript.show_all)}
          </button>
        </div>
      )}
    </div>
  );
}
