"use client";

import { useState, useMemo } from "react";
import { Pencil, Eye } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Markdown } from "../../common/markdown";

function isMarkdown(path: string) {
  return path.endsWith(".md") || path.endsWith(".mdx");
}

// ---------------------------------------------------------------------------
// YAML frontmatter parsing
// ---------------------------------------------------------------------------

interface Frontmatter {
  [key: string]: string;
}

const FRONTMATTER_RE = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?/;

function parseFrontmatter(raw: string): {
  frontmatter: Frontmatter | null;
  body: string;
} {
  const match = FRONTMATTER_RE.exec(raw);
  if (!match) return { frontmatter: null, body: raw };

  const yamlBlock = match[1]!;
  const body = raw.slice(match[0].length);
  const frontmatter: Frontmatter = {};

  for (const line of yamlBlock.split("\n")) {
    const idx = line.indexOf(":");
    if (idx === -1) continue;
    const key = line.slice(0, idx).trim();
    let value = line.slice(idx + 1).trim();
    // Strip surrounding quotes
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }
    if (key) frontmatter[key] = value;
  }

  return {
    frontmatter: Object.keys(frontmatter).length > 0 ? frontmatter : null,
    body,
  };
}

// ---------------------------------------------------------------------------
// Frontmatter display
// ---------------------------------------------------------------------------

function FrontmatterCard({ data }: { data: Frontmatter }) {
  return (
    <div className="mb-4 rounded-lg border bg-muted/30 px-4 py-3">
      <div className="grid gap-1.5">
        {Object.entries(data).map(([key, value]) => (
          <div key={key} className="flex gap-2 text-xs">
            <span className="shrink-0 font-medium text-muted-foreground min-w-[80px]">
              {key}
            </span>
            <span className="text-foreground">{value}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// File viewer
// ---------------------------------------------------------------------------

export function FileViewer({
  path,
  content,
  onChange,
}: {
  path: string;
  content: string;
  onChange: (content: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const isMd = isMarkdown(path);

  const { frontmatter, body } = useMemo(
    () => (isMd ? parseFrontmatter(content) : { frontmatter: null, body: content }),
    [content, isMd],
  );

  return (
    <div className="flex h-full flex-col">
      {/* File header */}
      <div className="flex h-10 items-center justify-between border-b px-4">
        <span className="text-xs font-mono text-muted-foreground truncate">
          {path}
        </span>
        <div className="flex items-center gap-1">
          {isMd && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => setEditing(!editing)}
                    className="text-muted-foreground"
                  >
                    {editing ? (
                      <Eye className="h-3.5 w-3.5" />
                    ) : (
                      <Pencil className="h-3.5 w-3.5" />
                    )}
                  </Button>
                }
              />
              <TooltipContent>
                {editing ? "Preview" : "Edit"}
              </TooltipContent>
            </Tooltip>
          )}
        </div>
      </div>

      {/* File content */}
      <div className="flex-1 min-h-0 overflow-y-auto">
        {isMd && !editing ? (
          <div className="p-6">
            {frontmatter && <FrontmatterCard data={frontmatter} />}
            <Markdown mode="full">
              {body || "*No content yet*"}
            </Markdown>
          </div>
        ) : (
          <Textarea
            value={content}
            onChange={(e) => onChange(e.target.value)}
            placeholder={
              isMd
                ? "Write markdown content..."
                : "File content..."
            }
            className="h-full min-h-full resize-none rounded-none border-0 font-mono text-sm leading-relaxed focus-visible:ring-0"
          />
        )}
      </div>
    </div>
  );
}
