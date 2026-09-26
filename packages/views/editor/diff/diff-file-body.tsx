"use client";

import { memo, useMemo, useState } from "react";
import { toHtml } from "hast-util-to-html";
import { splitHunkRows, type DiffFileView, type DiffLine } from "@multica/core/code-changes";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { highlightCode } from "../syntax-highlight";
import { extensionToLanguage } from "../utils/preview";
import type { DiffLayout } from "../viewer-chrome";
import "../styles/code.css";

// Past this many lines a file renders in pages: a generated lockfile would
// otherwise mount tens of thousands of rows at once.
const LINE_PAGE = 2000;
// Highlighting runs per line; beyond this many lines it costs more than it
// helps and the diff renders plain.
const HIGHLIGHT_LIMIT = 5000;

function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

/**
 * One file's hunks, unified or side by side. The file's own language drives
 * highlighting, line by line — a hunk is a fragment, so a string or comment
 * that opens before it reads as plain code, which is the usual diff tradeoff.
 */
export const DiffFileBody = memo(function DiffFileBody({
  file,
  layout,
}: {
  file: DiffFileView;
  layout: DiffLayout;
}) {
  const { t } = useT("editor");
  const [limit, setLimit] = useState(LINE_PAGE);
  const hunks = useMemo(() => file.hunks ?? [], [file.hunks]);
  const totalLines = hunks.reduce((n, h) => n + h.lines.length, 0);
  const language = extensionToLanguage(file.path);

  const html = useMemo(() => {
    const cache = new Map<DiffLine, string>();
    if (!language || totalLines > HIGHLIGHT_LIMIT) return cache;
    for (const hunk of hunks) {
      for (const line of hunk.lines) {
        try {
          cache.set(line, toHtml(highlightCode(line.text, language)) as string);
        } catch {
          cache.set(line, escapeHtml(line.text));
        }
      }
    }
    return cache;
  }, [hunks, language, totalLines]);

  if (file.binary) {
    return <DiffNotice>{t(($) => $.diff.binary)}</DiffNotice>;
  }
  if (file.hunks === null) {
    return <DiffNotice>{t(($) => $.diff.file_not_in_patch)}</DiffNotice>;
  }
  if (hunks.length === 0) {
    return <DiffNotice>{t(($) => $.diff.no_text_changes)}</DiffNotice>;
  }

  const code = (line: DiffLine) => {
    const highlighted = html.get(line);
    return highlighted !== undefined
      ? <span dangerouslySetInnerHTML={{ __html: highlighted || "&#8203;" }} />
      : <span>{line.text || "\u200b"}</span>;
  };

  let rendered = 0;
  const body: React.ReactNode[] = [];
  for (const [index, hunk] of hunks.entries()) {
    if (rendered >= limit) break;
    body.push(
      <div
        key={`h${index}`}
        className="border-y border-info/10 bg-info/8 px-4 py-1 text-muted-foreground first:border-t-0"
      >
        {hunk.header}
      </div>,
    );
    if (layout === "split") {
      const rows = splitHunkRows(hunk);
      for (const [r, row] of rows.entries()) {
        if (rendered >= limit) break;
        rendered++;
        body.push(
          <div key={`h${index}-${r}`} className="grid grid-cols-2">
            <SplitCell line={row.left} side="old" code={code} />
            <SplitCell line={row.right} side="new" code={code} bordered />
          </div>,
        );
      }
    } else {
      for (const [l, line] of hunk.lines.entries()) {
        if (rendered >= limit) break;
        rendered++;
        body.push(
          <div
            key={`h${index}-${l}`}
            className={cn(
              "grid grid-cols-[3.5rem_3.5rem_1.5rem_minmax(0,1fr)]",
              line.kind === "add" && "bg-success/10",
              line.kind === "del" && "bg-destructive/10",
            )}
          >
            <LineNumber value={line.oldNumber} />
            <LineNumber value={line.newNumber} />
            <Sign kind={line.kind} />
            <span className="whitespace-pre-wrap break-all pr-4">{code(line)}</span>
          </div>,
        );
      }
    }
  }

  return (
    <div className="transcript-code font-mono text-label leading-5 [font-variant-ligatures:none]">
      {body}
      {totalLines > limit && (
        <button
          type="button"
          className="block w-full border-t border-border px-4 py-2 text-left font-sans text-caption text-muted-foreground hover:bg-muted hover:text-foreground"
          onClick={() => setLimit((n) => n + LINE_PAGE)}
        >
          {t(($) => $.diff.show_more_lines, { count: totalLines - limit })}
        </button>
      )}
    </div>
  );
});

function LineNumber({ value }: { value: number | null }) {
  return (
    <span className="select-none pr-3 text-right tabular-nums text-faint-foreground">
      {value ?? ""}
    </span>
  );
}

function Sign({ kind }: { kind: DiffLine["kind"] }) {
  return (
    <span
      aria-hidden
      className={cn(
        "select-none text-center",
        kind === "add" && "text-success",
        kind === "del" && "text-destructive",
      )}
    >
      {kind === "add" ? "+" : kind === "del" ? "−" : ""}
    </span>
  );
}

function SplitCell({
  line,
  side,
  code,
  bordered,
}: {
  line: DiffLine | null;
  side: "old" | "new";
  code: (line: DiffLine) => React.ReactNode;
  bordered?: boolean;
}) {
  return (
    <div
      className={cn(
        "grid min-w-0 grid-cols-[3.5rem_1.5rem_minmax(0,1fr)]",
        bordered && "border-l border-border",
        !line && "bg-muted/40",
        line?.kind === "add" && "bg-success/10",
        line?.kind === "del" && "bg-destructive/10",
      )}
    >
      {line ? (
        <>
          <LineNumber value={side === "old" ? line.oldNumber : line.newNumber} />
          <Sign kind={line.kind} />
          <span className="whitespace-pre-wrap break-all pr-3">{code(line)}</span>
        </>
      ) : null}
    </div>
  );
}

function DiffNotice({ children }: { children: React.ReactNode }) {
  return (
    <p className="px-4 py-10 text-center text-body text-muted-foreground">{children}</p>
  );
}
