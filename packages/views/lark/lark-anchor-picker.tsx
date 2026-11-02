"use client";

import { Check, X } from "@multica/ui/icons";
import type { LarkMessageAnchor } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useLocale, useT } from "../i18n";
import { anchorTimeMs, chatIdSuffix, larkDiscoveryErrorKey } from "./discovery";
import { useLarkMessageAnchors, useLarkTargetCapabilities } from "./use-lark-discovery";
import {
  LarkDiscoveryErrorAlert,
  type LarkAnchorSelection,
} from "./picker-common";

function senderLabelKey(type: string): "sender_user" | "sender_app" | "sender_anonymous" | "sender_unknown" {
  switch (type) {
    case "user":
      return "sender_user";
    case "app":
      return "sender_app";
    case "anonymous":
      return "sender_anonymous";
    default:
      return "sender_unknown";
  }
}

function AnchorRow({
  anchor,
  selected,
  onSelect,
  disabled,
}: {
  anchor: LarkMessageAnchor;
  selected: boolean;
  onSelect: () => void;
  disabled?: boolean;
}) {
  const { t } = useT("lark");
  const locale = useLocale();
  const ms = anchorTimeMs(anchor.create_time);
  return (
    <div
      role="option"
      aria-selected={selected}
      aria-disabled={disabled === true}
      tabIndex={disabled ? -1 : 0}
      className={cn(
        "w-full px-3 py-2 text-left outline-none transition-colors",
        disabled
          ? "cursor-not-allowed opacity-60"
          : "cursor-pointer hover:bg-muted focus-visible:bg-muted",
        selected && "bg-muted/60",
      )}
      onClick={() => {
        if (!disabled) onSelect();
      }}
      onKeyDown={(e) => {
        if (disabled) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelect();
        }
      }}
    >
      <span className="flex items-start justify-between gap-2">
        {/* Summaries arrive pre-flattened and are rendered as plain text —
            never HTML/Markdown (contract). */}
        <span
          className={cn(
            "line-clamp-2 min-w-0 flex-1 text-body",
            selected ? "font-medium text-foreground" : "text-foreground",
          )}
        >
          {anchor.summary}
        </span>
        {selected && <Check className="mt-0.5 h-4 w-4 shrink-0 text-foreground" />}
      </span>
      <span className="mt-0.5 block text-micro text-muted-foreground">
        {ms != null
          ? new Date(ms).toLocaleString(locale)
          : t(($) => $.anchor.time_unknown)}
        {" · "}
        {/* Only the provider-returned sender id with its type is shown; no
            contact lookup or inferred name exists (contract). */}
        {t(($) => $.anchor[senderLabelKey(anchor.sender.type)])}
        {anchor.sender.id ? ` · …${chatIdSuffix(anchor.sender.id)}` : ""}
      </span>
    </div>
  );
}

function SelectedAnchorChip({
  value,
  onClear,
  disabled,
}: {
  value: LarkAnchorSelection;
  onClear: () => void;
  disabled?: boolean;
}) {
  const { t } = useT("lark");
  return (
    <div className="flex items-center justify-between gap-2 rounded-md border border-primary/40 bg-primary/5 px-3 py-2">
      <span className="min-w-0">
        <span className="block text-micro text-muted-foreground">
          {t(($) => $.anchor.selected)}
        </span>
        <span className="line-clamp-2 block text-body font-medium">
          {value.summary.trim() !== "" ? value.summary : value.messageId}
        </span>
      </span>
      <Button
        size="sm"
        variant="ghost"
        className="shrink-0"
        onClick={onClear}
        disabled={disabled}
        aria-label={t(($) => $.picker.clear)}
      >
        <X className="h-3.5 w-3.5" />
      </Button>
    </div>
  );
}

/**
 * Message-anchor picker for one already-selected group (OL-74): newest
 * first, manual "load earlier" paging, summary + localized time + sender per
 * row. Never auto-selects the latest message — a topic target exists only
 * after an explicit pick. The selected message_id (and optional thread_id)
 * goes into the EXISTING save/approval requests; nothing here saves.
 */
export function LarkAnchorPicker({
  wsId,
  installationId,
  chatId,
  value,
  onChange,
  disabled,
  fallback,
  enabled = true,
}: {
  wsId: string;
  installationId: string;
  /** Empty → the picker asks for a group first and issues no request. */
  chatId: string;
  value: LarkAnchorSelection | null;
  onChange: (selection: LarkAnchorSelection | null) => void;
  disabled?: boolean;
  fallback: React.ReactNode;
  enabled?: boolean;
}) {
  const { t } = useT("lark");
  const caps = useLarkTargetCapabilities(wsId, installationId, { enabled });
  const supported = caps.data?.message_anchor_list_supported === true;
  const list = useLarkMessageAnchors(wsId, installationId, chatId, {
    enabled: enabled && supported && chatId !== "",
  });

  if (chatId === "") {
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.anchor.pick_group_first)}
      </p>
    );
  }
  if (caps.isPending && enabled) {
    return (
      <p className="py-2 text-caption text-muted-foreground">
        {t(($) => $.anchor.loading)}
      </p>
    );
  }
  if (!supported) {
    // Forbidden is NOT a fallback case (OL-72 contract): explain the required
    // management role, never offer raw-ID entry as a bypass. A saved anchor
    // stays visible and keeps its existing save semantics.
    if (caps.isError && larkDiscoveryErrorKey(caps.error) === "forbidden") {
      return (
        <div className="space-y-2">
          {value != null && (
            <SelectedAnchorChip value={value} onClear={() => onChange(null)} disabled={disabled} />
          )}
          <p className="text-caption text-muted-foreground">
            {t(($) => $.picker.error.forbidden)}
          </p>
        </div>
      );
    }
    return (
      <div className="space-y-2">
        <p className="text-caption text-muted-foreground">
          {caps.isError
            ? t(($) => $.picker.error[larkDiscoveryErrorKey(caps.error)])
            : t(($) => $.picker.error.unsupported)}
        </p>
        {fallback}
      </div>
    );
  }

  const savedMissing =
    value != null &&
    list.errorKey == null &&
    !list.isLoading &&
    !list.hasMore &&
    !list.items.some((a) => a.message_id === value.messageId);

  return (
    <div className="space-y-2">
      {value != null && (
        <SelectedAnchorChip value={value} onClear={() => onChange(null)} disabled={disabled} />
      )}
      {list.errorKey != null && (
        <LarkDiscoveryErrorAlert
          errorKey={list.errorKey}
          onRetry={list.retry}
          onRestart={list.restart}
        />
      )}
      <div
        role="listbox"
        aria-label={t(($) => $.anchor.list_label)}
        className="max-h-56 divide-y overflow-y-auto rounded-md border"
      >
        {list.items.map((anchor) => (
          <AnchorRow
            key={anchor.message_id}
            anchor={anchor}
            selected={value?.messageId === anchor.message_id}
            disabled={disabled}
            onSelect={() =>
              onChange(
                value?.messageId === anchor.message_id
                  ? null
                  : {
                      messageId: anchor.message_id,
                      summary: anchor.summary,
                      threadId: anchor.thread_id,
                    },
              )
            }
          />
        ))}
        {list.isLoading && (
          <p className="px-3 py-4 text-center text-caption text-muted-foreground">
            {t(($) => $.anchor.loading)}
          </p>
        )}
        {!list.isLoading && !list.hasMore && list.items.length === 0 && list.errorKey == null && (
          <p className="px-3 py-4 text-center text-caption text-muted-foreground">
            {t(($) => $.anchor.empty)}
          </p>
        )}
      </div>
      {list.hasMore && (
        <Button
          size="sm"
          variant="outline"
          className="w-full"
          disabled={disabled || list.isFetchingMore}
          onClick={list.fetchMore}
        >
          {list.isFetchingMore
            ? t(($) => $.picker.loading_more)
            : t(($) => $.anchor.load_more)}
        </Button>
      )}
      {savedMissing && (
        <p role="status" className="text-caption text-warning">
          {t(($) => $.anchor.saved_missing)}
        </p>
      )}
    </div>
  );
}
