"use client";

import { useMemo } from "react";
import {
  FileCode2,
  FileText,
  FileImage,
  FileSpreadsheet,
  File,
} from "lucide-react";
import { detectFileCategory, type FileCategory } from "./utils/file-type";
import { NotebookViewer } from "./components/notebook-viewer";
import { CodeViewer } from "./components/code-viewer";
import { PdfViewer } from "./components/pdf-viewer";
import { ImageViewer } from "./components/image-viewer";
import { MarkdownViewer } from "./components/markdown-viewer";

// ---------------------------------------------------------------------------
// File icon helper
// ---------------------------------------------------------------------------

const CATEGORY_ICONS: Record<FileCategory, typeof File> = {
  notebook: FileSpreadsheet,
  markdown: FileText,
  pdf: FileText,
  code: FileCode2,
  image: FileImage,
  text: File,
};

function FileIcon({ category }: { category: FileCategory }) {
  const Icon = CATEGORY_ICONS[category];
  return <Icon className="size-3.5 text-muted-foreground" />;
}

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

export interface FileViewerProps {
  /** File path or name — used for type detection and display */
  path: string;

  /**
   * File content as a string.
   * For text-based formats (code, markdown, notebook JSON, etc.) pass the raw text.
   * For binary formats (PDF, images) pass a URL or data URI instead via `url`.
   */
  content?: string;

  /**
   * URL to the file. Used for PDF and image viewers.
   * If both `content` and `url` are provided, text-based viewers use `content`
   * and binary viewers use `url`.
   */
  url?: string;

  /** Callback when file content changes (markdown editing) */
  onChange?: (content: string) => void;

  /** When true, disables editing capabilities */
  readOnly?: boolean;
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export function FileViewer({
  path,
  content,
  url,
  onChange,
  readOnly = false,
}: FileViewerProps) {
  const category = useMemo(() => detectFileCategory(path), [path]);
  const fileName = useMemo(() => path.split("/").pop() ?? path, [path]);

  const renderViewer = () => {
    switch (category) {
      case "notebook":
        if (!content) return <EmptyState message="No notebook content provided" />;
        return <NotebookViewer content={content} />;

      case "markdown":
        return (
          <MarkdownViewer
            content={content ?? ""}
            onChange={onChange}
            readOnly={readOnly}
          />
        );

      case "pdf":
        if (!url) return <EmptyState message="No PDF URL provided" />;
        return <PdfViewer url={url} />;

      case "image":
        if (!url) return <EmptyState message="No image URL provided" />;
        return <ImageViewer url={url} alt={fileName} />;

      case "code":
        return <CodeViewer path={path} content={content ?? ""} />;

      case "text":
      default:
        return <CodeViewer path={path} content={content ?? ""} />;
    }
  };

  return (
    <div className="flex h-full flex-col rounded-lg border bg-background overflow-hidden">
      {/* File header */}
      <div className="flex h-10 items-center gap-2 border-b px-4 bg-muted/10">
        <FileIcon category={category} />
        <span className="text-xs font-mono text-muted-foreground truncate">
          {path}
        </span>
        <span className="ml-auto text-[10px] uppercase tracking-wider font-medium text-muted-foreground/50 rounded bg-muted/50 px-1.5 py-0.5">
          {category}
        </span>
      </div>

      {/* Viewer content */}
      <div className="flex-1 min-h-0">{renderViewer()}</div>
    </div>
  );
}

function EmptyState({ message }: { message: string }) {
  return (
    <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
      {message}
    </div>
  );
}
