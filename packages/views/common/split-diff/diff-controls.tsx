"use client";
import { useState } from "react";
import { Check, ChevronsDownUp, ChevronsUpDown, Columns2, Copy, Rows2 } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "../../i18n";
import type { GitHubSplitMode } from "./github-split-diff";

export function CopyPathButton({ path }: { path: string }) {
  const { t } = useT("agents");
  const [copied, setCopied] = useState(false);

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            data-testid="run-file-diff-copy"
            className="shrink-0 cursor-pointer rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
            aria-label={
              copied
                ? t(($) => $.transcript.files_copied)
                : t(($) => $.transcript.files_copy_path)
            }
            onClick={() => {
              void copyText(path).then((ok) => {
                if (!ok) return;
                setCopied(true);
                window.setTimeout(() => setCopied(false), 1500);
              });
            }}
          >
            {copied ? (
              <Check className="h-3.5 w-3.5 text-success" />
            ) : (
              <Copy className="h-3.5 w-3.5" />
            )}
          </button>
        }
      />
      <TooltipContent>
        {copied
          ? t(($) => $.transcript.files_copied)
          : t(($) => $.transcript.files_copy_path)}
      </TooltipContent>
    </Tooltip>
  );
}

export function DiffExpandControls({
  onExpandAll,
  onCollapseAll,
}: {
  onExpandAll: () => void;
  onCollapseAll: () => void;
}) {
  const { t } = useT("agents");
  return (
    <div
      role="group"
      data-testid="run-file-diff-expand"
      aria-label={t(($) => $.transcript.files_expand_collapse)}
      className="flex shrink-0 rounded-md border p-px"
    >
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              className="cursor-pointer rounded-sm p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
              aria-label={t(($) => $.transcript.files_expand_all)}
              onClick={onExpandAll}
            >
              <ChevronsUpDown className="h-3.5 w-3.5" />
            </button>
          }
        />
        <TooltipContent>
          {t(($) => $.transcript.files_expand_all)}
        </TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              className="cursor-pointer rounded-sm p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
              aria-label={t(($) => $.transcript.files_collapse_all)}
              onClick={onCollapseAll}
            >
              <ChevronsDownUp className="h-3.5 w-3.5" />
            </button>
          }
        />
        <TooltipContent>
          {t(($) => $.transcript.files_collapse_all)}
        </TooltipContent>
      </Tooltip>
    </div>
  );
}

export function DiffLayoutSwitch({
  mode,
  onChange,
}: {
  mode: GitHubSplitMode;
  onChange: (mode: GitHubSplitMode) => void;
}) {
  const { t } = useT("agents");
  return (
    <div
      role="radiogroup"
      data-testid="run-file-diff-layout"
      aria-label={t(($) => $.transcript.files_layout)}
      className="flex shrink-0 rounded-md border p-px"
    >
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              role="radio"
              aria-checked={mode === "unified"}
              aria-label={t(($) => $.transcript.files_layout_unified)}
              className={cn(
                "cursor-pointer rounded-sm p-1 text-muted-foreground hover:text-foreground",
                mode === "unified" && "bg-muted text-foreground",
              )}
              onClick={() => onChange("unified")}
            >
              <Rows2 className="h-3.5 w-3.5" />
            </button>
          }
        />
        <TooltipContent>
          {t(($) => $.transcript.files_layout_unified)}
        </TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              role="radio"
              aria-checked={mode === "split"}
              aria-label={t(($) => $.transcript.files_layout_split)}
              className={cn(
                "cursor-pointer rounded-sm p-1 text-muted-foreground hover:text-foreground",
                mode === "split" && "bg-muted text-foreground",
              )}
              onClick={() => onChange("split")}
            >
              <Columns2 className="h-3.5 w-3.5" />
            </button>
          }
        />
        <TooltipContent>
          {t(($) => $.transcript.files_layout_split)}
        </TooltipContent>
      </Tooltip>
    </div>
  );
}
