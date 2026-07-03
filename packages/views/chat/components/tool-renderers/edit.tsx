import { useMemo, useState } from "react";
import { ChevronRight } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { DiffBlock, computeLineDiff, diffStat } from "@multica/ui/markdown/DiffBlock";
import { CodeBlock } from "@multica/ui/markdown/CodeBlock";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import type { ChatTimelineItem } from "@multica/core/chat";
import { useT } from "../../../i18n";
import { shortenPath } from "./util";

function str(input: Record<string, unknown> | undefined, key: string): string {
  const v = input?.[key];
  return typeof v === "string" ? v : "";
}

/**
 * edit / write renderer. When the input carries the prior text
 * (old_string/new_string, the Edit tool) it shows a real unified diff with a
 * default-visible `+X/−Y` summary. When it only carries new content (the Write
 * tool creates a file), it falls back to a labeled highlighted block — a diff
 * against nothing would be all-green noise.
 */
export function EditToolBody({ item }: { item: ChatTimelineItem }) {
  const { t } = useT("chat");
  const [open, setOpen] = useState(false);
  const input = item.input;
  const filePath = str(input, "file_path");
  const oldString = str(input, "old_string");
  const newString = str(input, "new_string");
  const content = str(input, "content");

  const hasPriorText = oldString !== "" || newString !== "";

  const stat = useMemo(
    () => (hasPriorText ? diffStat(computeLineDiff(oldString, newString)) : null),
    [hasPriorText, oldString, newString],
  );

  if (!hasPriorText && content === "") {
    return null;
  }

  // Write (new file): labeled highlighted block, not a diff.
  if (!hasPriorText) {
    return (
      <Collapsible open={open} onOpenChange={setOpen}>
        <CollapsibleTrigger className="flex items-center gap-1 text-2xs text-muted-foreground hover:text-foreground transition-colors">
          <ChevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
          <span>
            {t(($) => $.tool.new_file)}
            {filePath ? ` · ${shortenPath(filePath)}` : ""}
          </span>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className="mt-0.5 max-h-[240px] overflow-auto rounded border bg-muted/40 p-2">
            <CodeBlock code={content} mode="minimal" />
          </div>
        </CollapsibleContent>
      </Collapsible>
    );
  }

  return (
    <div className="space-y-0.5">
      <div className="text-2xs tabular-nums text-muted-foreground">
        <span className="text-success">+{stat?.added ?? 0}</span>{" "}
        <span className="text-destructive">−{stat?.removed ?? 0}</span>
      </div>
      <Collapsible open={open} onOpenChange={setOpen}>
        <CollapsibleTrigger className="flex items-center gap-1 text-2xs text-muted-foreground hover:text-foreground transition-colors">
          <ChevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
          <span>{t(($) => $.tool.section_diff)}</span>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className="mt-0.5">
            <DiffBlock oldString={oldString} newString={newString} filePath={filePath || undefined} />
          </div>
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
}
