"use client";

import { X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

interface DocumentDiffViewerProps {
  oldContent: string;
  newContent: string;
  oldLabel: string;
  newLabel: string;
  onClose: () => void;
}

/**
 * Simple side-by-side diff viewer. Uses a basic line-by-line comparison.
 * For a more sophisticated diff, react-diff-viewer can be added later.
 */
export function DocumentDiffViewer({
  oldContent,
  newContent,
  oldLabel,
  newLabel,
  onClose,
}: DocumentDiffViewerProps) {
  const { t } = useT("documents");
  const oldLines = oldContent.split("\n");
  const newLines = newContent.split("\n");

  return (
    <div className="flex flex-1 flex-col">
      <div className="flex h-10 shrink-0 items-center justify-between border-b px-4">
        <h3 className="text-xs font-medium">{t(($) => $.diff.title)}</h3>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 w-6 p-0"
          onClick={onClose}
        >
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>
      <div className="grid flex-1 grid-cols-2 divide-x overflow-hidden">
        <div className="flex flex-col">
          <div className="shrink-0 border-b bg-muted/30 px-3 py-1.5">
            <span className="text-[10px] font-medium text-muted-foreground">
              {oldLabel}
            </span>
          </div>
          <ScrollArea className="flex-1">
            <pre className="p-3 text-xs leading-relaxed">
              {oldLines.map((line, i) => {
                const changed = i < newLines.length && line !== newLines[i];
                const removed = i >= newLines.length;
                return (
                  <div
                    key={i}
                    className={cn(
                      "px-1",
                      removed && "bg-destructive/10 text-destructive",
                      changed && "bg-warning/10",
                    )}
                  >
                    <span className="mr-3 inline-block w-6 text-right text-muted-foreground/40 select-none">
                      {i + 1}
                    </span>
                    {line || " "}
                  </div>
                );
              })}
            </pre>
          </ScrollArea>
        </div>
        <div className="flex flex-col">
          <div className="shrink-0 border-b bg-muted/30 px-3 py-1.5">
            <span className="text-[10px] font-medium text-muted-foreground">
              {newLabel}
            </span>
          </div>
          <ScrollArea className="flex-1">
            <pre className="p-3 text-xs leading-relaxed">
              {newLines.map((line, i) => {
                const changed = i < oldLines.length && line !== oldLines[i];
                const added = i >= oldLines.length;
                return (
                  <div
                    key={i}
                    className={cn(
                      "px-1",
                      added && "bg-green-500/10 text-green-700 dark:text-green-400",
                      changed && "bg-warning/10",
                    )}
                  >
                    <span className="mr-3 inline-block w-6 text-right text-muted-foreground/40 select-none">
                      {i + 1}
                    </span>
                    {line || " "}
                  </div>
                );
              })}
            </pre>
          </ScrollArea>
        </div>
      </div>
    </div>
  );
}
