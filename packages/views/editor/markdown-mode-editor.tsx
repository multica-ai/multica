"use client";

import { useEffect, useRef, useState } from "react";
import { Code2, Pilcrow } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { ContentEditor, type ContentEditorRef } from "./content-editor";

type MarkdownEditorMode = "rich" | "source";

export interface MarkdownModeEditorProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  labels: {
    rich: string;
    source: string;
  };
  className?: string;
  contentClassName?: string;
  richEditorClassName?: string;
  sourceEditorClassName?: string;
  debounceMs?: number;
  disableMentions?: boolean;
  autoFocus?: boolean;
}

export function MarkdownModeEditor({
  value,
  onChange,
  placeholder,
  labels,
  className,
  contentClassName,
  richEditorClassName,
  sourceEditorClassName,
  debounceMs = 300,
  disableMentions = false,
  autoFocus = false,
}: MarkdownModeEditorProps) {
  const [mode, setMode] = useState<MarkdownEditorMode>("rich");
  const editorRef = useRef<ContentEditorRef>(null);

  useEffect(() => {
    if (!autoFocus || mode !== "rich") return;
    const id = window.setTimeout(() => editorRef.current?.focus(), 0);
    return () => window.clearTimeout(id);
  }, [autoFocus, mode]);

  const switchMode = (nextMode: MarkdownEditorMode) => {
    if (nextMode === mode) return;
    setMode(nextMode);
  };

  return (
    <div className={cn("flex min-h-0 flex-col gap-2", className)}>
      <div className="flex justify-end">
        <div
          role="group"
          aria-label="Markdown editor mode"
          className="inline-flex rounded-md border bg-muted/20 p-0.5"
        >
          <Button
            type="button"
            variant={mode === "rich" ? "secondary" : "ghost"}
            size="sm"
            className="h-7 gap-1.5 px-2 text-xs"
            aria-pressed={mode === "rich"}
            onClick={() => switchMode("rich")}
          >
            <Pilcrow className="size-3.5" />
            {labels.rich}
          </Button>
          <Button
            type="button"
            variant={mode === "source" ? "secondary" : "ghost"}
            size="sm"
            className="h-7 gap-1.5 px-2 text-xs"
            aria-pressed={mode === "source"}
            onClick={() => switchMode("source")}
          >
            <Code2 className="size-3.5" />
            {labels.source}
          </Button>
        </div>
      </div>

      <div className={contentClassName}>
        {mode === "rich" ? (
          <ContentEditor
            ref={editorRef}
            defaultValue={value}
            onUpdate={onChange}
            placeholder={placeholder}
            debounceMs={debounceMs}
            disableMentions={disableMentions}
            flushPendingOnUnmount
            className={richEditorClassName}
          />
        ) : (
          <Textarea
            value={value}
            onChange={(event) => onChange(event.target.value)}
            placeholder={placeholder}
            autoFocus={autoFocus}
            spellCheck={false}
            className={cn(
              "h-full min-h-full resize-none font-mono text-xs leading-5",
              sourceEditorClassName,
            )}
          />
        )}
      </div>
    </div>
  );
}

export type { MarkdownEditorMode };
