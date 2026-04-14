"use client";

import { useMemo } from "react";
import { CodeBlock } from "@multica/ui/markdown";
import { Markdown } from "../../common/markdown";
import { parseNotebook, type NotebookCell, type CellOutput } from "../utils/notebook-parser";

// ---------------------------------------------------------------------------
// ANSI escape code stripping for error tracebacks
// ---------------------------------------------------------------------------

const ANSI_RE = /\x1b\[[0-9;]*m/g;
function stripAnsi(str: string): string {
  return str.replace(ANSI_RE, "");
}

// ---------------------------------------------------------------------------
// Cell output rendering
// ---------------------------------------------------------------------------

function OutputBlock({ output }: { output: CellOutput }) {
  if (output.outputType === "error") {
    const tb = output.traceback?.map(stripAnsi).join("\n") ?? `${output.ename}: ${output.evalue}`;
    return (
      <div className="rounded border border-red-300/50 bg-red-50/30 dark:border-red-800/50 dark:bg-red-950/20 p-3 overflow-x-auto">
        <pre className="font-mono text-xs text-red-700 dark:text-red-400 whitespace-pre-wrap">{tb}</pre>
      </div>
    );
  }

  if (output.html) {
    return (
      <div
        className="overflow-x-auto text-sm [&_table]:border-collapse [&_td]:border [&_td]:border-border/50 [&_td]:px-2 [&_td]:py-1 [&_th]:border [&_th]:border-border/50 [&_th]:px-2 [&_th]:py-1 [&_th]:bg-muted/50"
        dangerouslySetInnerHTML={{ __html: output.html }}
      />
    );
  }

  if (output.imageData && output.imageMimeType) {
    return (
      <div className="my-1">
        <img
          src={`data:${output.imageMimeType};base64,${output.imageData}`}
          alt="Cell output"
          className="max-w-full h-auto rounded"
        />
      </div>
    );
  }

  if (output.text) {
    return (
      <pre className="font-mono text-xs text-muted-foreground whitespace-pre-wrap overflow-x-auto">
        {output.text}
      </pre>
    );
  }

  return null;
}

// ---------------------------------------------------------------------------
// Individual cell
// ---------------------------------------------------------------------------

function CellView({
  cell,
  language,
}: {
  cell: NotebookCell;
  language: string;
}) {
  if (cell.cellType === "markdown") {
    return (
      <div className="px-4 py-3">
        <Markdown mode="full">{cell.source}</Markdown>
      </div>
    );
  }

  if (cell.cellType === "raw") {
    return (
      <div className="px-4 py-3">
        <pre className="font-mono text-sm whitespace-pre-wrap text-muted-foreground">{cell.source}</pre>
      </div>
    );
  }

  // Code cell
  return (
    <div className="group">
      {/* Code input */}
      <div className="flex">
        {/* Execution count gutter */}
        <div className="shrink-0 w-14 pt-3 text-right pr-2">
          <span className="font-mono text-xs text-muted-foreground/60">
            {cell.executionCount != null ? `[${cell.executionCount}]` : "[ ]"}
          </span>
        </div>
        {/* Code block */}
        <div className="flex-1 min-w-0">
          <CodeBlock code={cell.source} language={language} mode="full" />
        </div>
      </div>

      {/* Outputs */}
      {cell.outputs.length > 0 && (
        <div className="flex">
          <div className="shrink-0 w-14" />
          <div className="flex-1 min-w-0 space-y-2 pb-3 pr-4">
            {cell.outputs.map((output, i) => (
              <OutputBlock key={i} output={output} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Notebook viewer
// ---------------------------------------------------------------------------

export function NotebookViewer({ content }: { content: string }) {
  const notebook = useMemo(() => {
    try {
      return parseNotebook(content);
    } catch {
      return null;
    }
  }, [content]);

  if (!notebook) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
        Failed to parse notebook
      </div>
    );
  }

  return (
    <div className="divide-y">
      {/* Notebook metadata header */}
      <div className="flex items-center gap-3 px-4 py-2 bg-muted/20">
        <span className="text-xs font-medium text-muted-foreground">
          Kernel: {notebook.metadata.kernelName}
        </span>
        <span className="text-xs text-muted-foreground/60">|</span>
        <span className="text-xs text-muted-foreground">
          {notebook.cells.length} cells
        </span>
      </div>

      {/* Cells */}
      {notebook.cells.map((cell, i) => (
        <CellView key={i} cell={cell} language={notebook.metadata.language} />
      ))}
    </div>
  );
}
