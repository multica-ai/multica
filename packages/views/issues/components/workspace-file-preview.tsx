"use client";

import { useMemo } from "react";
import { FileText, Loader2 } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { CodeBlock } from "@multica/ui/markdown";
import { Markdown } from "../../common/markdown";
import { MermaidDiagram } from "./mermaid-diagram";
import "./workspace-file-preview.css";

// ---------------------------------------------------------------------------
// Language detection from file extension
// ---------------------------------------------------------------------------

const EXT_TO_LANG: Record<string, string> = {
  ts: "typescript",
  tsx: "tsx",
  js: "javascript",
  jsx: "jsx",
  py: "python",
  go: "go",
  rs: "rust",
  rb: "ruby",
  java: "java",
  kt: "kotlin",
  swift: "swift",
  c: "c",
  cpp: "cpp",
  h: "c",
  hpp: "cpp",
  css: "css",
  scss: "scss",
  html: "html",
  json: "json",
  yaml: "yaml",
  yml: "yaml",
  toml: "toml",
  xml: "xml",
  sql: "sql",
  sh: "bash",
  bash: "bash",
  zsh: "bash",
  dockerfile: "dockerfile",
  graphql: "graphql",
  prisma: "prisma",
  vue: "vue",
  svelte: "svelte",
};

function getLanguage(path: string): string | undefined {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  return EXT_TO_LANG[ext];
}

function isMarkdownFile(path: string): boolean {
  return path.endsWith(".md") || path.endsWith(".mdx");
}

// ---------------------------------------------------------------------------
// Markdown with mermaid support
// ---------------------------------------------------------------------------

/**
 * Wraps the shared Markdown component but intercepts ```mermaid code blocks
 * and renders them as diagrams. We do this by pre-processing the markdown
 * to replace mermaid blocks with a placeholder, then rendering the placeholders.
 */
function MarkdownWithMermaid({ content }: { content: string }) {
  // Extract mermaid blocks and replace with placeholders
  const { processed, mermaidBlocks } = useMemo(() => {
    const blocks: string[] = [];
    const text = content.replace(
      /```mermaid\n([\s\S]*?)```/g,
      (_match, code: string) => {
        const index = blocks.length;
        blocks.push(code.trim());
        return `\n<div data-mermaid-index="${index}"></div>\n`;
      },
    );
    return { processed: text, mermaidBlocks: blocks };
  }, [content]);

  if (mermaidBlocks.length === 0) {
    return <Markdown mode="full">{content}</Markdown>;
  }

  // Split content at mermaid placeholders and render interleaved
  const parts = processed.split(/<div data-mermaid-index="(\d+)"><\/div>/);

  return (
    <div>
      {parts.map((part, i) => {
        // Even indices are markdown text, odd are mermaid block indices
        if (i % 2 === 0) {
          if (!part.trim()) return null;
          return <Markdown key={i} mode="full">{part}</Markdown>;
        }
        const blockIndex = parseInt(part, 10);
        const code = mermaidBlocks[blockIndex];
        if (!code) return null;
        return <MermaidDiagram key={`mermaid-${blockIndex}`} code={code} />;
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Public component
// ---------------------------------------------------------------------------

export function WorkspaceFilePreview({
  path,
  content,
  loading,
  diff,
  diffMode,
}: {
  path: string | null;
  content: { content: string; path: string; size: number; mtime: string } | null;
  loading: boolean;
  /** When present and diffMode is on, render the diff instead of raw content. */
  diff?: {
    path: string;
    status: string;
    diff: string | null;
    content: string | null;
  } | null;
  diffMode?: boolean;
}) {
  if (!path) {
    return (
      <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
        <FileText className="h-8 w-8 text-muted-foreground/30" />
        <p className="mt-3 text-sm">Select a file to preview</p>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="flex h-full flex-col">
        <div className="flex h-10 items-center border-b px-4">
          <span className="text-xs font-mono text-muted-foreground truncate">
            {path}
          </span>
        </div>
        <div className="flex flex-1 items-center justify-center">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      </div>
    );
  }

  // ── Diff mode ────────────────────────────────────────────────────────────
  if (diffMode) {
    if (!diff) {
      return (
        <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
          <FileText className="h-8 w-8 text-muted-foreground/30" />
          <p className="mt-3 text-sm">Unable to load diff</p>
        </div>
      );
    }

    // Untracked file — daemon returned the raw content instead of a diff.
    const isUntracked = diff.status === "?" || diff.status === "A";
    const body = diff.diff ?? diff.content ?? "";
    const lang = isUntracked && diff.content ? getLanguage(diff.path) : "diff";

    return (
      <div className="flex h-full flex-col">
        <div className="flex h-10 items-center justify-between border-b px-4">
          <span className="text-xs font-mono text-muted-foreground truncate">
            {diff.path}
          </span>
          <span className="ml-2 shrink-0 rounded bg-accent px-1.5 py-0.5 text-[10px] uppercase tracking-wider text-accent-foreground">
            {diff.status === "?" ? "New" : diff.status === "A" ? "Added" : diff.status === "D" ? "Deleted" : diff.status === "R" ? "Renamed" : "Modified"}
          </span>
        </div>
        <div className="flex-1 min-h-0 overflow-y-auto">
          {body ? (
            <CodeBlock
              code={body}
              language={lang}
              mode="full"
              className="rounded-none border-0"
            />
          ) : (
            <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
              No changes in this file
            </div>
          )}
        </div>
      </div>
    );
  }

  // ── Content mode ─────────────────────────────────────────────────────────
  if (!content) {
    return (
      <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
        <FileText className="h-8 w-8 text-muted-foreground/30" />
        <p className="mt-3 text-sm">Unable to load file</p>
      </div>
    );
  }

  const isMd = isMarkdownFile(content.path);
  const lang = getLanguage(content.path);

  return (
    <div className="flex h-full flex-col">
      {/* File header */}
      <div className="flex h-10 items-center border-b px-4">
        <span className="text-xs font-mono text-muted-foreground truncate">
          {content.path}
        </span>
      </div>

      {/* File content */}
      <div className={cn("flex-1 min-h-0 overflow-y-auto", isMd && "p-6")}>
        {isMd ? (
          <div className="workspace-markdown-preview">
            <MarkdownWithMermaid content={content.content} />
          </div>
        ) : (
          <CodeBlock
            code={content.content}
            language={lang}
            mode="full"
            className="rounded-none border-0"
          />
        )}
      </div>
    </div>
  );
}
