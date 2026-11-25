"use client";

import { Fragment, useLayoutEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ChevronDown, UnfoldVertical } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { highlightBlock, highlightToLines } from "../task-transcript/diff-highlight";
import {
  SPLIT_UNIFIED_BREAKPOINT,
  VIRTUALIZE_MIN_ITEMS,
  type DiffViewItem,
  type SplitDiffCell,
  type SplitDiffRow,
} from "./split-diff-model";
import "../../editor/styles/code.css";

export type GitHubSplitMode = "split" | "unified";

/** `sign` keeps +/- after the line number. `tint` is GitHub: no sign, gutter wash. */
export type GitHubSplitGutter = "sign" | "tint";

type SideHtml = { left?: string; right?: string };

function TabArrow() {
  return (
    <span data-diff-tab="" className="diff-tab select-none text-faint-foreground" aria-hidden="true">
      →
    </span>
  );
}

function TabbedText({ text }: { text: string }) {
  if (!text.includes("\t")) return text;
  const chunks = text.split("\t");
  return (
    <>
      {chunks.map((chunk, index) => (
        <Fragment key={index}>
          {index > 0 ? <TabArrow /> : null}
          {chunk}
        </Fragment>
      ))}
    </>
  );
}

function decorateTabsHtml(html: string): string {
  if (!html.includes("\t")) return html;
  return html.replaceAll(
    "\t",
    '<span data-diff-tab="" class="diff-tab select-none text-faint-foreground" aria-hidden="true">→</span>',
  );
}

function cellHasWordHighlight(cell: SplitDiffCell): boolean {
  return (
    cell.kind !== "empty" &&
    cell.parts.some((part) => part.type === "equal") &&
    cell.parts.some((part) => part.type !== "equal")
  );
}

/** lowlight output only: the hast serializer escapes text; sole elements are `hljs-*` spans. */
function HighlightedHtml({ html }: { html: string }) {
  return <span className="hljs" dangerouslySetInnerHTML={{ __html: decorateTabsHtml(html) }} />;
}

function highlightedPart(text: string, language: string | undefined) {
  const html = language ? highlightBlock(text, language) : null;
  return html ? <HighlightedHtml html={html} /> : <TabbedText text={text} />;
}

function DiffParts({
  cell,
  language,
  lineHtml,
}: {
  cell: SplitDiffCell;
  language?: string;
  lineHtml?: string;
}) {
  if (cell.kind === "empty") return null;
  // Line text stays the default foreground.
  // Word-level ins/del keep a wash; whole-line add/remove only tint the row
  // (and GitHub gutter), not the entire line as a word hit.
  if (!cellHasWordHighlight(cell)) {
    if (lineHtml) return <HighlightedHtml html={lineHtml} />;
    return highlightedPart(cell.text, language);
  }
  return (
    <>
      {cell.parts.map((part, index) => {
        const body = highlightedPart(part.text, language);
        if (part.type === "equal") {
          return <span key={index}>{body}</span>;
        }
        if (part.type === "remove") {
          return (
            <del
              key={index}
              className="rounded-xs bg-destructive/30 px-[2px] text-foreground no-underline [box-decoration-break:clone]"
            >
              {body}
            </del>
          );
        }
        return (
          <ins
            key={index}
            className="rounded-xs bg-success/30 px-[2px] text-foreground no-underline [box-decoration-break:clone]"
          >
            {body}
          </ins>
        );
      })}
    </>
  );
}

function buildItemHighlights(items: DiffViewItem[], language: string | undefined): (SideHtml | undefined)[] {
  if (!language) return items.map(() => undefined);
  const leftLines: string[] = [];
  const rightLines: string[] = [];
  for (const item of items) {
    if (item.type !== "row") continue;
    if (item.row.left.kind !== "empty") leftLines.push(item.row.left.text);
    if (item.row.right.kind !== "empty") rightLines.push(item.row.right.text);
  }
  const leftHtml = leftLines.length > 0 ? highlightToLines(leftLines.join("\n"), language) : null;
  const rightHtml = rightLines.length > 0 ? highlightToLines(rightLines.join("\n"), language) : null;
  let leftIndex = 0;
  let rightIndex = 0;
  return items.map((item) => {
    if (item.type !== "row") return undefined;
    const html: SideHtml = {};
    if (item.row.left.kind !== "empty") html.left = leftHtml?.[leftIndex++];
    if (item.row.right.kind !== "empty") html.right = rightHtml?.[rightIndex++];
    return html;
  });
}

function signFor(kind: SplitDiffCell["kind"]): string {
  if (kind === "remove") return "-";
  if (kind === "add") return "+";
  return "";
}

function DiffSide({
  cell,
  line,
  side,
  withBorder,
  language,
  html,
  gutter = "sign",
}: {
  cell: SplitDiffCell;
  line: number | null;
  side: "before" | "after";
  withBorder?: boolean;
  language?: string;
  html?: string;
  gutter?: GitHubSplitGutter;
}) {
  const empty = cell.kind === "empty";
  const tint = gutter === "tint";
  return (
    <div
      data-change-diff-side={side}
      data-change-diff-kind={cell.kind}
      data-change-diff-gutter={gutter}
      className={cn(
        "flex min-w-0 items-stretch border-b",
        withBorder && "sm:border-r",
        cell.kind === "remove" && "bg-destructive/10",
        cell.kind === "add" && "bg-success/10",
        empty && "bg-muted/30",
      )}
    >
      <span
        data-change-diff-line={line ?? ""}
        className={cn(
          "flex w-10 shrink-0 items-start justify-end self-stretch border-r py-0.5 pr-1.5 font-mono text-micro leading-5 tabular-nums select-none",
          tint && cell.kind === "remove" && "bg-destructive/20 text-destructive",
          tint && cell.kind === "add" && "bg-success/20 text-success",
          tint && cell.kind === "equal" && "bg-muted/80 text-muted-foreground",
          tint && empty && "bg-muted/30 text-muted-foreground",
          !tint && "bg-muted/80 text-muted-foreground",
        )}
      >
        {line ?? ""}
      </span>
      {tint ? null : (
        <span
          data-change-diff-sign={signFor(cell.kind) || " "}
          className={cn(
            "flex w-4 shrink-0 items-start justify-center self-stretch py-0.5 font-mono text-caption leading-5 select-none",
            cell.kind === "remove" && "text-destructive",
            cell.kind === "add" && "text-success",
          )}
        >
          {signFor(cell.kind)}
        </span>
      )}
      <pre className="min-h-[1.5rem] min-w-0 flex-1 px-1 py-0.5 font-mono text-caption leading-5 text-foreground whitespace-pre-wrap break-all [overflow-wrap:anywhere]">
        {empty ? null : <DiffParts cell={cell} language={language} lineHtml={html} />}
      </pre>
    </div>
  );
}

function hunkMarker(item: { hunk: { oldStart: number; oldCount: number; newStart: number; newCount: number } }): string {
  return `@@ -${item.hunk.oldStart},${item.hunk.oldCount} +${item.hunk.newStart},${item.hunk.newCount} @@`;
}

function CollapsePane({
  item,
  withBorder,
}: {
  item: Extract<DiffViewItem, { type: "collapse" }>;
  withBorder?: boolean;
}) {
  const Icon = item.trailing ? ChevronDown : UnfoldVertical;
  const marker = hunkMarker(item);
  return (
    <span
      className={cn(
        "flex min-w-0 items-start border-b bg-muted py-1.5 text-caption text-muted-foreground",
        withBorder && "sm:border-r",
        item.trailing && "py-1",
      )}
    >
      <span className="flex w-10 shrink-0 justify-center pt-0.5">
        <Icon className="size-3.5" aria-hidden="true" />
      </span>
      {item.trailing ? null : (
        <span className="min-w-0 pr-2">
          <span className="font-mono">{marker}</span>
          {item.preview ? (
            <span className="mt-0.5 block truncate font-mono text-micro">{item.preview}</span>
          ) : null}
        </span>
      )}
    </span>
  );
}

function CollapseBar({
  item,
  mode,
  onExpand,
  expandUnchangedLabel,
}: {
  item: Extract<DiffViewItem, { type: "collapse" }>;
  mode: GitHubSplitMode;
  onExpand: () => void;
  expandUnchangedLabel: (count: number) => string;
}) {
  return (
    <button
      type="button"
      data-change-diff-collapse={item.hidden.length}
      className={cn("w-full text-left hover:brightness-[0.98]", mode === "split" && "grid grid-cols-2")}
      aria-label={expandUnchangedLabel(item.hidden.length)}
      onClick={onExpand}
    >
      <CollapsePane item={item} withBorder={mode === "split"} />
      {mode === "split" ? <CollapsePane item={item} /> : null}
    </button>
  );
}

function HunkBar({
  item,
  mode,
}: {
  item: Extract<DiffViewItem, { type: "hunk" }>;
  mode: GitHubSplitMode;
}) {
  const marker = hunkMarker(item);
  const pane = (
    <span
      className={cn(
        "flex min-w-0 items-start border-b bg-muted py-1.5 font-mono text-caption text-muted-foreground",
        mode === "split" && "sm:border-r",
      )}
    >
      <span className="flex w-10 shrink-0 justify-center pt-0.5">
        <UnfoldVertical className="size-3.5" aria-hidden="true" />
      </span>
      <span className="min-w-0 pr-2">
        <span>{marker}</span>
        {item.preview ? (
          <span className="mt-0.5 block truncate font-mono text-micro">{item.preview}</span>
        ) : null}
      </span>
    </span>
  );
  if (mode === "unified") return pane;
  return (
    <div className="grid grid-cols-2">
      {pane}
      <span className="flex min-w-0 items-start border-b bg-muted py-1.5" />
    </div>
  );
}

function SplitRow({
  row,
  highlight,
  language,
  gutter,
}: {
  row: SplitDiffRow;
  highlight?: SideHtml;
  language?: string;
  gutter?: GitHubSplitGutter;
}) {
  return (
    <div className="grid grid-cols-2">
      <DiffSide
        cell={row.left}
        line={row.leftLine}
        side="before"
        withBorder
        language={language}
        html={highlight?.left}
        gutter={gutter}
      />
      <DiffSide
        cell={row.right}
        line={row.rightLine}
        side="after"
        language={language}
        html={highlight?.right}
        gutter={gutter}
      />
    </div>
  );
}

function UnifiedRow({
  row,
  highlight,
  language,
  gutter,
}: {
  row: SplitDiffRow;
  highlight?: SideHtml;
  language?: string;
  gutter?: GitHubSplitGutter;
}) {
  if (row.left.kind === "equal" && row.right.kind === "equal") {
    return (
      <DiffSide
        cell={row.right}
        line={row.rightLine}
        side="after"
        language={language}
        html={highlight?.right}
        gutter={gutter}
      />
    );
  }
  return (
    <>
      {row.left.kind !== "empty" ? (
        <DiffSide
          cell={row.left}
          line={row.leftLine}
          side="before"
          language={language}
          html={highlight?.left}
          gutter={gutter}
        />
      ) : null}
      {row.right.kind !== "empty" && row.right.kind !== "equal" ? (
        <DiffSide
          cell={row.right}
          line={row.rightLine}
          side="after"
          language={language}
          html={highlight?.right}
          gutter={gutter}
        />
      ) : null}
    </>
  );
}

function DiffItemView({
  item,
  mode,
  highlight,
  language,
  gutter,
  onExpandCollapse,
  expandUnchangedLabel,
}: {
  item: DiffViewItem;
  mode: GitHubSplitMode;
  highlight?: SideHtml;
  language?: string;
  gutter?: GitHubSplitGutter;
  onExpandCollapse: (id: string) => void;
  expandUnchangedLabel: (count: number) => string;
}) {
  if (item.type === "collapse") {
    return (
      <CollapseBar
        item={item}
        mode={mode}
        expandUnchangedLabel={expandUnchangedLabel}
        onExpand={() => onExpandCollapse(item.id)}
      />
    );
  }
  if (item.type === "hunk") {
    return <HunkBar item={item} mode={mode} />;
  }
  if (mode === "unified") {
    return <UnifiedRow row={item.row} highlight={highlight} language={language} gutter={gutter} />;
  }
  return <SplitRow row={item.row} highlight={highlight} language={language} gutter={gutter} />;
}

export function useContainerSplitMode(containerRef: RefObject<HTMLElement | null>): {
  mode: GitHubSplitMode;
  width: number;
} {
  const [width, setWidth] = useState(0);

  useLayoutEffect(() => {
    const el = containerRef.current;
    if (!el) return undefined;

    const read = () => {
      const measured = el.getBoundingClientRect().width || el.clientWidth;
      setWidth((current) => (current === measured ? current : measured));
    };
    read();
    const observer = new ResizeObserver(read);
    observer.observe(el);
    window.addEventListener("resize", read);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", read);
    };
  }, [containerRef]);

  return {
    // jsdom reports 0; keep the desktop default until we have a real width.
    mode: width > 0 && width < SPLIT_UNIFIED_BREAKPOINT ? "unified" : "split",
    width,
  };
}

export function GitHubSplitDiff({
  items,
  mode = "split",
  gutter = "sign",
  virtualize,
  language,
  expandUnchangedLabel,
  onExpandCollapse,
  className,
}: {
  items: DiffViewItem[];
  mode?: GitHubSplitMode;
  gutter?: GitHubSplitGutter;
  virtualize?: boolean;
  language?: string;
  expandUnchangedLabel: (count: number) => string;
  onExpandCollapse: (id: string) => void;
  className?: string;
}) {
  const parentRef = useRef<HTMLDivElement>(null);
  const enableVirtual = virtualize === true || (virtualize !== false && items.length >= VIRTUALIZE_MIN_ITEMS);
  const highlights = useMemo(() => buildItemHighlights(items, language), [items, language]);
  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => parentRef.current,
    estimateSize: (index) => (items[index]?.type === "row" ? 24 : 40),
    overscan: 12,
    enabled: enableVirtual,
  });

  const renderItem = (item: DiffViewItem, index: number) => (
    <DiffItemView
      item={item}
      mode={mode}
      highlight={highlights[index]}
      language={language}
      gutter={gutter}
      onExpandCollapse={onExpandCollapse}
      expandUnchangedLabel={expandUnchangedLabel}
    />
  );

  if (!enableVirtual) {
    return (
      <div
        data-github-split-diff=""
        data-github-split-mode={mode}
        className={cn("transcript-code overflow-x-auto", className)}
      >
        {items.map((item, index) => (
          <div key={itemKey(item, index)}>{renderItem(item, index)}</div>
        ))}
      </div>
    );
  }

  return (
    <div
      ref={parentRef}
      data-github-split-diff=""
      data-github-split-mode={mode}
      className={cn("transcript-code max-h-[min(32rem,50vh)] overflow-auto", className)}
    >
      <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
        {virtualizer.getVirtualItems().map((virtualRow) => {
          const item = items[virtualRow.index];
          if (!item) return null;
          return (
            <div
              key={virtualRow.key}
              data-index={virtualRow.index}
              ref={virtualizer.measureElement}
              className="absolute top-0 left-0 w-full"
              style={{ transform: `translateY(${virtualRow.start}px)` }}
            >
              {renderItem(item, virtualRow.index)}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function itemKey(item: DiffViewItem, index: number): string {
  if (item.type === "collapse" || item.type === "hunk") return item.id;
  return `row-${index}-${item.row.leftLine ?? "x"}-${item.row.rightLine ?? "x"}`;
}

export function DiffStat({ added, removed }: { added: number; removed: number }) {
  if (added === 0 && removed === 0) return null;
  return (
    <span className="ml-auto flex shrink-0 items-center gap-1.5 font-mono text-caption tabular-nums">
      {added > 0 ? (
        <span data-change-diff-stat="added" className="text-success">
          +{added}
        </span>
      ) : null}
      {removed > 0 ? (
        <span data-change-diff-stat="removed" className="text-destructive">
          -{removed}
        </span>
      ) : null}
    </span>
  );
}

export function GitHubSplitDiffHeader({
  label,
  added,
  removed,
  actions,
}: {
  label: ReactNode;
  actions?: ReactNode;
  added: number;
  removed: number;
}) {
  return (
    <div className="flex items-center gap-2 border-b bg-muted/30 px-3 py-2">
      <span className="min-w-0 truncate text-caption font-medium">{label}</span>
      <DiffStat added={added} removed={removed} />
      {actions}
    </div>
  );
}

export function useExpandedCollapseIds() {
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  const expand = (id: string) => {
    setExpanded((current) => new Set([...current, id]));
  };
  return [expanded, expand] as const;
}

export function useRenderedDiffItems(collapsed: DiffViewItem[], expanded: ReadonlySet<string>): DiffViewItem[] {
  return useMemo(() => {
    const rendered: DiffViewItem[] = [];
    for (const item of collapsed) {
      if (item.type === "row" || item.type === "hunk") {
        rendered.push(item);
        continue;
      }
      if (expanded.has(item.id)) {
        for (const row of item.hidden) rendered.push({ type: "row", row });
        continue;
      }
      rendered.push(item);
    }
    return rendered;
  }, [collapsed, expanded]);
}
