import type { ReactNode } from "react";
import type { ChatTimelineItem } from "@multica/core/chat";
import { ToolStatusChip } from "./status-chip";
import { GenericToolBody } from "./generic";
import { BashToolBody } from "./bash";
import { ReadToolBody } from "./read";
import { EditToolBody } from "./edit";
import { getToolSummary } from "./util";

/** Renders the body (below the shared header) of a single tool card. */
export type ToolRenderer = (item: ChatTimelineItem) => ReactNode;

/**
 * tool name (lowercased) → purpose-built body renderer. Unmapped tools fall
 * back to GenericToolBody, so nothing ever renders worse than the old generic
 * row.
 */
export const toolRenderers: Record<string, ToolRenderer> = {
  bash: (item) => <BashToolBody item={item} />,
  read: (item) => <ReadToolBody item={item} />,
  edit: (item) => <EditToolBody item={item} />,
  write: (item) => <EditToolBody item={item} />,
};

export function renderToolBody(item: ChatTimelineItem): ReactNode {
  const key = item.tool?.toLowerCase();
  const renderer = key ? toolRenderers[key] : undefined;
  return renderer ? renderer(item) : <GenericToolBody item={item} />;
}

/**
 * A merged tool card: one visually light row (left accent rule + subtle bg, not
 * an elevated card) whose header answers "what + did it work" with zero clicks
 * — tool name, input summary, and the status chip — and whose body is the
 * purpose-built (or generic) renderer. The paired tool_result was folded into
 * the tool_use upstream (build-timeline), so this single card replaces the old
 * call row + result row.
 */
export function ToolCard({ item }: { item: ChatTimelineItem }) {
  const summary = getToolSummary(item);
  const status = item.status ?? "done";
  const body = renderToolBody(item);

  return (
    <div className="rounded-r border-l-2 border-border/60 bg-muted/10 py-0.5 pl-2">
      <div className="flex items-center gap-1.5 text-xs">
        <span className="shrink-0 font-medium text-foreground">{item.tool}</span>
        {summary && <span className="truncate text-muted-foreground">{summary}</span>}
        <ToolStatusChip status={status} durationMs={item.duration_ms} className="ml-auto shrink-0" />
      </div>
      {body && <div className="mt-0.5">{body}</div>}
    </div>
  );
}
