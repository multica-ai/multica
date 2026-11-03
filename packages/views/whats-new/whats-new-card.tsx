"use client";

import { useState } from "react";
import { ChevronRight, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { useT } from "../i18n";
import { HighlightActionButton } from "./highlight-action";
import { highlightCopy, type WhatsNewRelease } from "./releases";

/**
 * The card that opens by itself after an update: every highlight released
 * since the user's last dismissal, one expanded and the rest as rows.
 */
export function WhatsNewCard({
  releases,
  onDismiss,
  onViewDetails,
}: {
  /** Unread releases, newest first. */
  releases: WhatsNewRelease[];
  onDismiss: () => void;
  onViewDetails: () => void;
}) {
  const { t, i18n } = useT("layout");
  const items = releases.flatMap((r) =>
    r.highlights.map((highlight) => ({ key: `${r.version}:${highlight.id}`, highlight })),
  );
  const [selectedKey, setSelectedKey] = useState(items[0]?.key);
  const selected = items.find((item) => item.key === selectedKey) ?? items[0];
  if (!selected) return null;

  const newest = releases[0]!.version;
  const oldest = releases[releases.length - 1]!.version;
  const range = newest === oldest ? `v${newest}` : `v${oldest} → v${newest}`;
  const selectedCopy = highlightCopy(selected.highlight, i18n.language);

  return (
    <div className="flex flex-col" data-slot="whats-new-card">
      <div className="flex items-start gap-2 pt-3.5 pr-2 pb-2.5 pl-4">
        <div className="min-w-0 flex-1">
          <p className="text-caption font-medium text-brand">
            {t(($) => $.whats_new.card.eyebrow, { range })}
          </p>
          <h2 className="mt-0.5 text-title-sm font-semibold">
            {t(($) => $.whats_new.card.title, { count: items.length })}
          </h2>
        </div>
        <IconButton label={t(($) => $.whats_new.card.dismiss)} onClick={onDismiss}>
          <X />
        </IconButton>
      </div>

      <div className="max-h-[min(60vh,440px)] overflow-y-auto px-1.5 pb-1">
        {selected.highlight.media && (
          <div className="mx-1.5 mb-1.5 overflow-hidden rounded-lg bg-muted ring-1 ring-surface-border">
            <img src={selected.highlight.media.src} alt="" className="h-40 w-full object-cover" />
          </div>
        )}
        <div className="rounded-md bg-accent px-3 pt-2.5 pb-3">
          <h3 className="text-body font-semibold">{selectedCopy.title}</h3>
          <p className="mt-0.5 text-label text-muted-foreground">{selectedCopy.body}</p>
          {selected.highlight.action && (
            <div className="mt-2.5">
              <HighlightActionButton action={selected.highlight.action} variant="button" onDone={onDismiss} />
            </div>
          )}
        </div>
        <ul className="mt-1">
          {items
            .filter((item) => item.key !== selected.key)
            .map((item) => (
              <li key={item.key}>
                <button
                  type="button"
                  onClick={() => setSelectedKey(item.key)}
                  className="flex h-[34px] w-full cursor-pointer items-center gap-2.5 rounded-md px-3 text-left outline-none hover:bg-accent focus-visible:bg-accent"
                >
                  <span aria-hidden className="size-1.5 shrink-0 rounded-full bg-brand" />
                  <span className="min-w-0 flex-1 truncate text-body font-medium">
                    {highlightCopy(item.highlight, i18n.language).title}
                  </span>
                  <ChevronRight className="size-3.5 shrink-0 text-faint-foreground" />
                </button>
              </li>
            ))}
        </ul>
      </div>

      <div className="flex items-center justify-end gap-1 border-t border-border py-2.5 pr-3 pl-4">
        <Button variant="ghost" size="sm" onClick={onViewDetails}>
          {t(($) => $.whats_new.card.view_details)}
        </Button>
        <Button size="sm" onClick={onDismiss}>
          {t(($) => $.whats_new.card.got_it)}
        </Button>
      </div>
    </div>
  );
}
