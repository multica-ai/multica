"use client";

import { useState, useMemo } from "react";
import { Pencil, Eye } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import {
  parseFrontmatter,
  type SkillFrontmatter,
} from "@multica/core/skills/frontmatter";
import { Markdown } from "../../common/markdown";
import { useT } from "../../i18n";

function isMarkdown(path: string) {
  return path.endsWith(".md") || path.endsWith(".mdx");
}

// ---------------------------------------------------------------------------
// Frontmatter display
// ---------------------------------------------------------------------------

function FrontmatterCard({ data }: { data: SkillFrontmatter }) {
  return (
    <div className="mb-4 rounded-lg border bg-muted/30 px-4 py-3">
      <div className="grid gap-1.5">
        {Object.entries(data).map(([key, value]) => (
          <div key={key} className="flex gap-2 text-xs">
            <span className="shrink-0 font-medium text-muted-foreground min-w-[80px]">
              {key}
            </span>
            <span className="text-foreground whitespace-pre-wrap break-words">
              {value.trimEnd()}
            </span>
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
  const { t } = useT("skills");
  const [editing, setEditing] = useState(false);
  const isMd = isMarkdown(path);

  const { frontmatter, body } = useMemo(
    () => (isMd ? parseFrontmatter(content) : { frontmatter: null, body: content }),
    [content, isMd],
  );

  return (
    <div className="flex h-full flex-col">
      {/* File header */}
      <div className="flex h-10 items-center justify-between gap-3 border-b px-4">
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
                {editing
                  ? t(($) => $.file_viewer.preview_tooltip)
                  : t(($) => $.file_viewer.edit_tooltip)}
              </TooltipContent>
            </Tooltip>
          )}
        </div>
      </div>

      {/* File content */}
      <div className="flex-1 min-h-0 overflow-y-auto">
        {isMd && !editing ? (
          <div className="p-4 sm:p-6">
            {frontmatter && <FrontmatterCard data={frontmatter} />}
            <Markdown mode="full">
              {body || t(($) => $.file_viewer.no_content)}
            </Markdown>
          </div>
        ) : (
          <Textarea
            value={content}
            onChange={(e) => onChange(e.target.value)}
            placeholder={
              isMd
                ? t(($) => $.file_viewer.markdown_placeholder)
                : t(($) => $.file_viewer.raw_placeholder)
            }
            className="h-full min-h-full resize-none rounded-none border-0 font-mono text-sm leading-relaxed focus-visible:ring-0"
          />
        )}
      </div>
    </div>
  );
}
