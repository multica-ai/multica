import { useState } from "react";
import { ChevronRight } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { CodeBlock } from "@multica/ui/markdown/CodeBlock";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import type { ChatTimelineItem } from "@multica/core/chat";
import { useT } from "../../../i18n";

/**
 * read renderer. The card header already shows the (shortened) file path; the
 * body adds a default-visible line range and an expandable content preview.
 */
export function ReadToolBody({ item }: { item: ChatTimelineItem }) {
  const { t } = useT("chat");
  const [open, setOpen] = useState(false);
  const input = item.input;
  const offset = typeof input?.offset === "number" ? input.offset : undefined;
  const limit = typeof input?.limit === "number" ? input.limit : undefined;
  let range = "";
  if (offset !== undefined && limit !== undefined) range = t(($) => $.tool.lines_range, { start: offset, end: offset + limit - 1 });
  else if (offset !== undefined) range = t(($) => $.tool.lines_from, { start: offset });
  else if (limit !== undefined) range = t(($) => $.tool.lines_first, { count: limit });
  const output = item.output ?? "";

  return (
    <div className="space-y-0.5">
      {range && <div className="text-2xs text-muted-foreground tabular-nums">{range}</div>}
      {output && (
        <Collapsible open={open} onOpenChange={setOpen}>
          <CollapsibleTrigger className="flex items-center gap-1 text-2xs text-muted-foreground/70 hover:text-foreground transition-colors">
            <ChevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
            <span>{t(($) => $.tool.section_preview)}</span>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div className="mt-0.5 max-h-[200px] overflow-auto rounded border bg-muted/40 p-2">
              <CodeBlock code={output.length > 4000 ? output.slice(0, 4000) + "\n… (truncated)" : output} mode="minimal" />
            </div>
          </CollapsibleContent>
        </Collapsible>
      )}
    </div>
  );
}
