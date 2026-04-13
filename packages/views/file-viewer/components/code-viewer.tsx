"use client";

import { useMemo } from "react";
import { Copy, Check } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { CodeBlock } from "@multica/ui/markdown";
import { getLanguageFromPath } from "../utils/file-type";
import { useState, useCallback } from "react";

export function CodeViewer({
  path,
  content,
}: {
  path: string;
  content: string;
}) {
  const language = useMemo(() => getLanguageFromPath(path), [path]);
  const [copied, setCopied] = useState(false);

  const lineCount = useMemo(() => content.split("\n").length, [content]);

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(content);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      console.error("Failed to copy:", err);
    }
  }, [content]);

  return (
    <div className="flex h-full flex-col">
      {/* Header bar */}
      <div className="flex items-center justify-between border-b px-4 py-1.5 bg-muted/20">
        <div className="flex items-center gap-3">
          <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            {language}
          </span>
          <span className="text-xs text-muted-foreground/60">
            {lineCount} lines
          </span>
        </div>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={handleCopy}
                className="text-muted-foreground hover:text-foreground"
                aria-label="Copy file"
              >
                {copied ? (
                  <Check className="size-3.5 text-success" />
                ) : (
                  <Copy className="size-3.5" />
                )}
              </Button>
            }
          />
          <TooltipContent>Copy file</TooltipContent>
        </Tooltip>
      </div>

      {/* Code content with line numbers */}
      <div className="flex-1 min-h-0 overflow-auto">
        <div className="flex">
          {/* Line number gutter */}
          <div className="shrink-0 select-none border-r bg-muted/10 px-3 py-3 text-right">
            {Array.from({ length: lineCount }, (_, i) => (
              <div key={i} className="font-mono text-xs leading-5 text-muted-foreground/40">
                {i + 1}
              </div>
            ))}
          </div>
          {/* Highlighted code */}
          <div className="flex-1 min-w-0 p-3 overflow-x-auto">
            <CodeBlock code={content} language={language} mode="minimal" />
          </div>
        </div>
      </div>
    </div>
  );
}
