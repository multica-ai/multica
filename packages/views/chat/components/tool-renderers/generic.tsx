import { useState } from "react";
import { ChevronRight } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import type { ChatTimelineItem } from "@multica/core/chat";

/**
 * Fallback body for tools without a purpose-built renderer. Preserves the
 * historical behaviour — collapsible raw input JSON plus the (paired) output —
 * so an unmapped tool never renders worse than before.
 */
export function GenericToolBody({ item }: { item: ChatTimelineItem }) {
  const [openInput, setOpenInput] = useState(false);
  const [openOutput, setOpenOutput] = useState(false);
  const hasInput = !!item.input && Object.keys(item.input).length > 0;
  const output = item.output ?? "";

  return (
    <div className="space-y-0.5">
      {hasInput && (
        <Collapsible open={openInput} onOpenChange={setOpenInput}>
          <CollapsibleTrigger className="flex items-center gap-1 text-2xs text-muted-foreground/70 hover:text-foreground transition-colors">
            <ChevronRight className={cn("size-3 transition-transform", openInput && "rotate-90")} />
            <span>input</span>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <pre className="mt-0.5 max-h-32 overflow-auto rounded bg-muted/50 p-2 text-xs text-muted-foreground whitespace-pre-wrap break-all">
              {JSON.stringify(item.input, null, 2)}
            </pre>
          </CollapsibleContent>
        </Collapsible>
      )}
      {output && (
        <Collapsible open={openOutput} onOpenChange={setOpenOutput}>
          <CollapsibleTrigger className="flex w-full items-start gap-1 text-2xs text-muted-foreground/70 hover:text-foreground transition-colors">
            <ChevronRight className={cn("size-3 shrink-0 mt-0.5 transition-transform", openOutput && "rotate-90")} />
            <span className="truncate text-left">
              {output.length > 120 ? output.slice(0, 120) + "..." : output}
            </span>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <pre className="mt-0.5 max-h-40 overflow-auto rounded bg-muted/50 p-2 text-xs text-muted-foreground whitespace-pre-wrap break-all">
              {output.length > 4000 ? output.slice(0, 4000) + "\n... (truncated)" : output}
            </pre>
          </CollapsibleContent>
        </Collapsible>
      )}
    </div>
  );
}
