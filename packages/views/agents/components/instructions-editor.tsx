"use client";

import { useState } from "react";
import { ChevronDown, FileText, X } from "lucide-react";
import { MarkdownModeEditor } from "../../editor";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

interface InstructionsEditorProps {
  /** Markdown source. Used both as default value when expanded and as
   *  preview text when collapsed. */
  value: string;
  /** Fires on every source-mode keystroke and on rich-editor updates. */
  onChange: (value: string) => void;
  /** Optional placeholder override. Defaults to the i18n "click to write"
   *  copy; the create dialog passes the duplicate-specific string for
   *  agents being cloned. */
  placeholder?: string;
}

/**
 * Collapsible Instructions field for the create-agent dialog. Stays compact
 * until the user wants to write — most agents only need instructions when
 * they're being authored carefully, not on every quick-create.
 *
 * Two states:
 *   collapsed → small clickable card, shows a preview of `value` (or the
 *               placeholder when empty). One click expands.
 *   expanded  → markdown editor with rich-text and source modes. "Collapse"
 *               button on the right of the header tucks it back; value is
 *               preserved.
 */
export function InstructionsEditor({
  value,
  onChange,
  placeholder,
}: InstructionsEditorProps) {
  const { t } = useT("agents");
  const [expanded, setExpanded] = useState(false);

  const label = t(($) => $.create_dialog.instructions.label);
  const resolvedPlaceholder =
    placeholder ?? t(($) => $.create_dialog.instructions.placeholder_blank);

  const expand = () => {
    setExpanded(true);
  };

  if (!expanded) {
    return (
      <div>
        <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {label}
        </div>
        <button
          type="button"
          onClick={expand}
          className="mt-1.5 flex w-full items-start gap-2.5 rounded-lg border bg-card px-3 py-3 text-left transition-colors hover:border-primary/40 hover:bg-accent/40"
        >
          <FileText className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            {value.trim() ? (
              // Preview: first 2 lines of markdown, ellipsised.
              <div className="line-clamp-2 whitespace-pre-wrap text-sm text-foreground/80">
                {value}
              </div>
            ) : (
              <div className="text-sm text-muted-foreground">{resolvedPlaceholder}</div>
            )}
          </div>
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground/40" />
        </button>
      </div>
    );
  }

  return (
    <div>
      <div className="flex items-center justify-between">
        <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {label}
        </div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => setExpanded(false)}
          className="h-6 gap-1 px-2 text-xs"
        >
          <X className="h-3 w-3" />
          {t(($) => $.create_dialog.instructions.collapse)}
        </Button>
      </div>
      <div
        className={cn(
          "mt-1.5 rounded-lg border bg-card",
          "focus-within:border-primary/40",
        )}
      >
        <MarkdownModeEditor
          value={value}
          onChange={onChange}
          placeholder={t(($) => $.create_dialog.instructions.editor_placeholder)}
          labels={{
            rich: t(($) => $.tab_body.instructions.mode_rich),
            source: t(($) => $.tab_body.instructions.mode_source),
          }}
          disableMentions
          autoFocus
          contentClassName="min-h-[160px] max-h-[320px] overflow-hidden px-3 py-2.5"
          richEditorClassName="min-h-[160px] max-h-[320px] overflow-y-auto text-sm"
          sourceEditorClassName="min-h-[160px] max-h-[320px] border-0 bg-transparent p-0 shadow-none focus-visible:ring-0"
        />
      </div>
    </div>
  );
}
