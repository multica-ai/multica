"use client";

import {
  Children,
  cloneElement,
  isValidElement,
  useMemo,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
} from "react";
import ReactMarkdown, {
  defaultUrlTransform,
  type Components,
} from "react-markdown";
import rehypeKatex from "rehype-katex";
import rehypeRaw from "rehype-raw";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import remarkBreaks from "remark-breaks";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import { MessageSquarePlus, Send, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { api } from "@multica/core/api";
import type { Attachment } from "@multica/core/types";
import { useT } from "../i18n";
import type { MarkdownAnnotationDraft } from "./markdown-annotation-types";
import { formatMarkdownAnnotationsComment } from "./markdown-annotation-comment";
import { selectionToMarkdownSourceSelection } from "./markdown-source-position";
import "katex/dist/katex.min.css";
import "./styles/index.css";

interface MarkdownAnnotationPreviewProps {
  attachmentId: string;
  filename: string;
  content: string;
  issueId?: string | null;
  attachments?: Attachment[];
  className?: string;
}

interface SourceNode {
  position?: {
    start?: { offset?: number };
    end?: { offset?: number };
  };
}

interface SourceCursor {
  value: number;
  limit: number;
}

interface SourceChunk {
  text: string;
  sourceStart: number;
  highlighted: boolean;
}

type MappedTagName =
  | "p"
  | "h1"
  | "h2"
  | "h3"
  | "h4"
  | "h5"
  | "h6"
  | "li"
  | "blockquote"
  | "td"
  | "th"
  | "code";

const sanitizeSchema = {
  ...defaultSchema,
  tagNames: [...(defaultSchema.tagNames ?? []), "mark"],
  protocols: {
    ...defaultSchema.protocols,
    href: [...(defaultSchema.protocols?.href ?? []), "mention", "slash"],
  },
  attributes: {
    ...defaultSchema.attributes,
    code: [
      ...(defaultSchema.attributes?.code ?? []),
      ["className", /^language-/],
      ["className", /^math-/],
      ["className", /^hljs/],
    ],
    img: [
      ...(defaultSchema.attributes?.img ?? []),
      "alt",
    ],
  },
};

function urlTransform(url: string): string {
  if (url.startsWith("mention://")) return url;
  if (url.startsWith("slash://skill/")) return url;
  return defaultUrlTransform(url);
}

function sourceRangeForNode(node: SourceNode | undefined, source: string): [number, number] {
  const start = node?.position?.start?.offset;
  const end = node?.position?.end?.offset;
  return [
    typeof start === "number" ? Math.max(0, Math.min(start, source.length)) : 0,
    typeof end === "number" ? Math.max(0, Math.min(end, source.length)) : source.length,
  ];
}

function findVisibleTextOffset(source: string, text: string, cursor: SourceCursor): number {
  if (!text) return cursor.value;
  const from = Math.max(0, Math.min(cursor.value, source.length));
  const limit = Math.max(from, Math.min(cursor.limit, source.length));
  const localIndex = source.slice(from, limit).indexOf(text);
  return localIndex >= 0 ? from + localIndex : from;
}

function sourceChunks(
  text: string,
  sourceStart: number,
  annotations: MarkdownAnnotationDraft[],
): SourceChunk[] {
  if (!text) return [];
  const boundaries = new Set<number>([0, text.length]);
  const sourceEnd = sourceStart + text.length;

  annotations.forEach((annotation) => {
    const annStart = annotation.range.start.offset;
    const annEnd = annotation.range.end.offset + 1;
    if (annEnd <= sourceStart || annStart >= sourceEnd) return;
    boundaries.add(Math.max(0, annStart - sourceStart));
    boundaries.add(Math.min(text.length, annEnd - sourceStart));
  });

  const sorted = [...boundaries].sort((a, b) => a - b);
  const chunks: SourceChunk[] = [];
  for (let i = 0; i < sorted.length - 1; i++) {
    const start = sorted[i]!;
    const end = sorted[i + 1]!;
    if (end <= start) continue;
    const chunkStart = sourceStart + start;
    const chunkEnd = sourceStart + end;
    const highlighted = annotations.some((annotation) => {
      const annStart = annotation.range.start.offset;
      const annEnd = annotation.range.end.offset + 1;
      return annStart < chunkEnd && annEnd > chunkStart;
    });
    chunks.push({
      text: text.slice(start, end),
      sourceStart: chunkStart,
      highlighted,
    });
  }
  return chunks;
}

function mappedText(
  text: string,
  sourceStart: number,
  annotations: MarkdownAnnotationDraft[],
): ReactNode[] {
  const parts = text.split(/(\r?\n)/);
  let partStart = sourceStart;
  const nodes: ReactNode[] = [];

  parts.forEach((part) => {
    if (!part) return;
    sourceChunks(part, partStart, annotations).forEach((chunk) => {
      const attrs = {
        "data-md-start": chunk.sourceStart,
        "data-md-end": chunk.sourceStart + chunk.text.length,
      };
      if (chunk.highlighted) {
        nodes.push(
          <mark
            key={`${chunk.sourceStart}:${chunk.text}`}
            {...attrs}
            className="rounded-sm bg-yellow-200 px-0.5 text-foreground dark:bg-yellow-500/35"
          >
            {chunk.text}
          </mark>,
        );
      } else {
        nodes.push(
          <span key={`${chunk.sourceStart}:${chunk.text}`} {...attrs}>
            {chunk.text}
          </span>,
        );
      }
    });
    partStart += part.length;
  });

  return nodes;
}

function hasSourceMapping(element: ReactElement): boolean {
  const props = element.props as Record<string, unknown>;
  return props["data-md-start"] != null && props["data-md-end"] != null;
}

function sourceMappedChildren(
  children: ReactNode,
  source: string,
  annotations: MarkdownAnnotationDraft[],
  cursor: SourceCursor,
): ReactNode {
  return Children.map(children, (child) => {
    if (typeof child === "string" || typeof child === "number") {
      const text = String(child);
      const start = findVisibleTextOffset(source, text, cursor);
      cursor.value = start + text.length;
      return mappedText(text, start, annotations);
    }
    if (!isValidElement(child)) return child;
    if (hasSourceMapping(child)) return child;

    const props = child.props as { children?: ReactNode };
    if (props.children == null) return child;
    return cloneElement(
      child as ReactElement<{ children?: ReactNode }>,
      undefined,
      sourceMappedChildren(props.children, source, annotations, cursor),
    );
  });
}

function renderMappedChildren(
  node: SourceNode | undefined,
  source: string,
  annotations: MarkdownAnnotationDraft[],
  children: ReactNode,
): ReactNode {
  const [start, limit] = sourceRangeForNode(node, source);
  return sourceMappedChildren(children, source, annotations, { value: start, limit });
}

function buildMarkdownComponents(
  source: string,
  annotations: MarkdownAnnotationDraft[],
): Partial<Components> {
  const mapped =
    (tag: MappedTagName) =>
      ({ node, children, ...props }: any) => {
        const Tag = tag as any;
        return (
          <Tag {...props}>
            {renderMappedChildren(node, source, annotations, children)}
          </Tag>
        );
      };

  return {
    p: mapped("p"),
    h1: mapped("h1"),
    h2: mapped("h2"),
    h3: mapped("h3"),
    h4: mapped("h4"),
    h5: mapped("h5"),
    h6: mapped("h6"),
    li: mapped("li"),
    blockquote: mapped("blockquote"),
    td: mapped("td"),
    th: mapped("th"),
    code: mapped("code"),
    table: ({ children }) => (
      <div className="tableWrapper">
        <table>{children}</table>
      </div>
    ),
  };
}

function rangeSummary(annotation: MarkdownAnnotationDraft): string {
  const { range, filename } = annotation;
  return `${filename}:L${range.start.line}:C${range.start.character}-L${range.end.line}:C${range.end.character}`;
}

export function MarkdownAnnotationPreview({
  attachmentId,
  filename,
  content,
  issueId,
  className,
}: MarkdownAnnotationPreviewProps) {
  const { t } = useT("editor");
  const rootRef = useRef<HTMLDivElement>(null);
  const [pending, setPending] = useState<Pick<MarkdownAnnotationDraft, "range" | "quote"> | null>(null);
  const [note, setNote] = useState("");
  const [annotations, setAnnotations] = useState<MarkdownAnnotationDraft[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const components = useMemo(
    () => buildMarkdownComponents(content, annotations),
    [content, annotations],
  );
  const enabled = !!issueId;

  const captureSelection = () => {
    if (!enabled || !rootRef.current) return;
    const next = selectionToMarkdownSourceSelection(rootRef.current, content, window.getSelection());
    setPending(next);
    setNote("");
  };

  const savePending = () => {
    if (!pending) return;
    const trimmed = note.trim();
    if (!trimmed) {
      toast.error(t(($) => $.annotation.empty_note));
      return;
    }
    setAnnotations((prev) => [
      ...prev,
      {
        id: `${Date.now()}-${prev.length}`,
        attachmentId,
        filename,
        quote: pending.quote,
        range: pending.range,
        note: trimmed,
        createdAt: Date.now(),
      },
    ]);
    setPending(null);
    setNote("");
    window.getSelection()?.removeAllRanges();
  };

  const sendAnnotations = async () => {
    if (!issueId || annotations.length === 0 || submitting) return;
    setSubmitting(true);
    try {
      await api.createComment(issueId, formatMarkdownAnnotationsComment(filename, annotations));
      setAnnotations([]);
      setPending(null);
      toast.success(t(($) => $.annotation.sent));
    } catch {
      toast.error(t(($) => $.annotation.send_failed));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className={cn("flex h-full min-h-0 flex-col", className)}>
      {enabled && (
        <div className="flex items-center gap-2 border-b border-border bg-background px-4 py-2">
          <span className="text-sm text-muted-foreground">
            {t(($) => $.annotation.count, { count: annotations.length })}
          </span>
          <div className="ml-auto flex items-center gap-1">
            {annotations.length > 0 && (
              <button
                type="button"
                className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-sm text-muted-foreground hover:bg-muted hover:text-foreground"
                onClick={() => setAnnotations([])}
              >
                <Trash2 className="size-3.5" />
                {t(($) => $.annotation.clear)}
              </button>
            )}
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded-md border border-border px-2 py-1 text-sm disabled:cursor-not-allowed disabled:opacity-50"
              disabled={annotations.length === 0 || submitting}
              onClick={sendAnnotations}
            >
              <Send className="size-3.5" />
              {t(($) => $.annotation.send_to_comments)}
            </button>
          </div>
        </div>
      )}

      {pending && (
        <div className="border-b border-border bg-muted/30 px-4 py-3">
          <div className="mb-2 flex items-center gap-2 text-sm font-medium">
            <MessageSquarePlus className="size-4" />
            {t(($) => $.annotation.add)}
          </div>
          <blockquote className="mb-2 max-h-20 overflow-hidden border-l-2 border-border pl-3 text-sm text-muted-foreground">
            {pending.quote}
          </blockquote>
          <textarea
            className="min-h-20 w-full resize-y rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
            placeholder={t(($) => $.annotation.note_placeholder)}
            value={note}
            onChange={(event) => setNote(event.target.value)}
          />
          <div className="mt-2 flex justify-end gap-2">
            <button type="button" className="rounded-md px-2 py-1 text-sm hover:bg-muted" onClick={() => setPending(null)}>
              {t(($) => $.annotation.cancel)}
            </button>
            <button type="button" className="rounded-md border border-border px-2 py-1 text-sm" onClick={savePending}>
              {t(($) => $.annotation.save)}
            </button>
          </div>
        </div>
      )}

      <div
        ref={rootRef}
        className="rich-text-editor readonly min-h-0 flex-1 overflow-auto px-6 py-4 text-sm"
        onMouseUp={captureSelection}
        data-testid="markdown-annotation-source"
      >
        <ReactMarkdown
          remarkPlugins={[
            [remarkMath, { singleDollarTextMath: false }],
            remarkBreaks,
            [remarkGfm, { singleTilde: false }],
          ]}
          rehypePlugins={[rehypeRaw, [rehypeSanitize, sanitizeSchema], rehypeKatex]}
          urlTransform={urlTransform}
          components={components}
        >
          {content}
        </ReactMarkdown>
      </div>

      {enabled && annotations.length > 0 && (
        <div className="max-h-44 overflow-auto border-t border-border bg-muted/20 px-4 py-3">
          <div className="mb-2 text-sm font-medium">{t(($) => $.annotation.list_title)}</div>
          <ol className="space-y-2 text-sm">
            {annotations.map((annotation) => (
              <li key={annotation.id} className="rounded-md border border-border bg-background p-2">
                <div className="font-mono text-xs text-muted-foreground">{rangeSummary(annotation)}</div>
                <div className="mt-1 text-muted-foreground">"{annotation.quote}"</div>
                <div className="mt-1">{annotation.note}</div>
              </li>
            ))}
          </ol>
        </div>
      )}
    </div>
  );
}
