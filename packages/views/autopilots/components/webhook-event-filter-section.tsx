"use client";

import { useState } from "react";
import { X, Plus, Filter, ExternalLink } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { WebhookEventFilter } from "@multica/core/types";
import { useT } from "../../i18n";

interface WebhookEventFilterSectionProps {
  filters: WebhookEventFilter[];
  onChange: (filters: WebhookEventFilter[]) => void;
}

export function WebhookEventFilterSection({
  filters,
  onChange,
}: WebhookEventFilterSectionProps) {
  const { t, i18n } = useT("autopilots");
  const [newEvent, setNewEvent] = useState("");
  const [newActions, setNewActions] = useState("");
  const docsHref = i18n.language?.startsWith("zh")
    ? `https://multica.ai/docs/zh/autopilots#${encodeURIComponent("事件过滤")}`
    : "https://multica.ai/docs/autopilots#event-filters";

  const addFilter = () => {
    const event = newEvent.trim();
    if (!event) return;
    const actions = newActions
      .split(",")
      .map((a) => a.trim())
      .filter((a) => a.length > 0);
    const next: WebhookEventFilter = { event };
    if (actions.length > 0) next.actions = actions;
    onChange([...filters, next]);
    setNewEvent("");
    setNewActions("");
  };

  const removeFilter = (idx: number) => {
    onChange(filters.filter((_, i) => i !== idx));
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-1.5 text-[11px] font-semibold tracking-[0.08em] text-muted-foreground uppercase">
        <Filter className="size-3" />
        {t(($) => $.dialog.event_filter_label)}
        <a
          href={docsHref}
          target="_blank"
          rel="noopener noreferrer"
          aria-label={t(($) => $.dialog.event_filter_docs_link_label)}
          title={t(($) => $.dialog.event_filter_docs_link_label)}
          className="ml-0.5 inline-flex items-center text-muted-foreground/80 hover:text-foreground transition-colors"
        >
          <ExternalLink className="size-3" />
        </a>
      </div>

      {filters.length > 0 && (
        <div className="space-y-1">
          {filters.map((f, idx) => (
            <div
              key={idx}
              className="flex items-center gap-2 rounded-md border bg-background px-2.5 py-1.5 text-xs"
            >
              <span className="font-mono font-medium text-foreground">
                {f.event}
              </span>
              {f.actions && f.actions.length > 0 && (
                <span className="text-muted-foreground">
                  : {f.actions.join(", ")}
                </span>
              )}
              <button
                type="button"
                onClick={() => removeFilter(idx)}
                className="ml-auto rounded p-0.5 text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
              >
                <X className="size-3" />
              </button>
            </div>
          ))}
        </div>
      )}

      <div className="flex gap-2">
        <input
          type="text"
          value={newEvent}
          onChange={(e) => setNewEvent(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              addFilter();
            }
          }}
          placeholder={t(($) => $.dialog.event_filter_event_placeholder)}
          className="flex-1 min-w-0 rounded-md border bg-background px-2.5 py-1.5 text-xs font-mono outline-none focus:ring-1 focus:ring-ring"
        />
        <input
          type="text"
          value={newActions}
          onChange={(e) => setNewActions(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              addFilter();
            }
          }}
          placeholder={t(($) => $.dialog.event_filter_actions_placeholder)}
          className="w-28 rounded-md border bg-background px-2.5 py-1.5 text-xs outline-none focus:ring-1 focus:ring-ring"
        />
        <button
          type="button"
          onClick={addFilter}
          disabled={!newEvent.trim()}
          className={cn(
            "inline-flex items-center justify-center rounded-md border px-2.5 py-1.5 text-xs font-medium transition-colors",
            newEvent.trim()
              ? "bg-background text-foreground hover:bg-accent/40 cursor-pointer"
              : "bg-muted text-muted-foreground cursor-not-allowed",
          )}
        >
          <Plus className="size-3.5" />
        </button>
      </div>
      <p className="text-[10px] text-muted-foreground">
        {t(($) => $.dialog.event_filter_hint)}
      </p>
    </div>
  );
}
