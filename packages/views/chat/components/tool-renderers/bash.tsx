import { useState } from "react";
import { ChevronRight, Copy, Check } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { copyText } from "@multica/ui/lib/clipboard";
import { CodeBlock } from "@multica/ui/markdown/CodeBlock";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import type { ChatTimelineItem } from "@multica/core/chat";
import { lastLines } from "./util";

/** Focusable copy button (visible focus ring via Button) for a text blob. */
function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-xs"
      aria-label={label}
      className="absolute right-1 top-1 text-muted-foreground hover:text-foreground"
      onClick={async (e) => {
        e.stopPropagation();
        if (await copyText(text)) {
          setCopied(true);
          setTimeout(() => setCopied(false), 2000);
        }
      }}
    >
      {copied ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
    </Button>
  );
}

/**
 * bash renderer. The failure case is where a zero-click preview earns its keep,
 * so the last few output lines (which usually carry the error) are always
 * visible and rendered in the error color when the tool failed — the command
 * reads as failed at a glance, matching the header's error chip. The full
 * output expands into a scrollable (~200px) monospace pane with a focusable
 * copy button.
 *
 * Note: Claude's bash tool_result folds stdout+stderr into one string and does
 * not expose a separate exit code, so failure is signalled by is_error (→ the
 * error chip + tinted preview) rather than a parsed exit status.
 */
export function BashToolBody({ item }: { item: ChatTimelineItem }) {
  const [open, setOpen] = useState(false);
  const output = item.output ?? "";
  if (!output) return null;

  const isError = item.status === "error";
  const tail = lastLines(output, 3).join("\n");

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-start gap-1 rounded text-left text-2xs hover:bg-accent/30 transition-colors">
        <ChevronRight className={cn("mt-0.5 size-3 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")} />
        <pre
          className={cn(
            "min-w-0 flex-1 truncate whitespace-pre-wrap break-all font-mono",
            isError ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {tail}
        </pre>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="relative mt-0.5 max-h-[200px] overflow-auto rounded border bg-muted/40 p-2">
          <CodeBlock code={output} mode="minimal" language="bash" />
          <CopyButton text={output} label="Copy output" />
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}
