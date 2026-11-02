"use client";

import { Check, Search, X } from "@multica/ui/icons";
import type { LarkDiscoveredChat } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { larkDiscoveryErrorKey } from "./discovery";
import { useLarkTargetCapabilities, useLarkTargetChats } from "./use-lark-discovery";
import {
  ChatIdDisclosure,
  LarkChatAvatar,
  LarkDiscoveryErrorAlert,
  type LarkChatSelection,
} from "./picker-common";

/** One group row, shared by the single-select picker and the multi-select.
 * Rows are divs with listbox semantics (not <button>) because they host the
 * nested ID-disclosure toggle; selection works with click and Enter/Space. */
export function LarkChatRow({
  chat,
  selected,
  onSelect,
  disabled,
}: {
  chat: LarkDiscoveredChat;
  selected: boolean;
  onSelect: () => void;
  disabled?: boolean;
}) {
  const { t } = useT("lark");
  const name = chat.name.trim() !== "" ? chat.name : t(($) => $.picker.unnamed);
  return (
    <div
      role="option"
      aria-selected={selected}
      aria-disabled={disabled === true}
      tabIndex={disabled ? -1 : 0}
      className={cn(
        "flex w-full items-start gap-2.5 px-3 py-2 text-left outline-none transition-colors",
        disabled
          ? "cursor-not-allowed opacity-60"
          : "cursor-pointer hover:bg-muted focus-visible:bg-muted",
        // The selected state lives on weight/color so hover never visually
        // downgrades it (repo UI rule).
        selected && "bg-muted/60 font-medium text-foreground data-[active]:bg-muted",
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
      <LarkChatAvatar name={name} avatar={chat.avatar} />
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-1.5">
          <span className="truncate text-body">{name}</span>
          {chat.external === true && (
            <Badge variant="secondary" className="shrink-0">
              {t(($) => $.picker.external_badge)}
            </Badge>
          )}
        </span>
        {chat.description.trim() !== "" && (
          <span className="block truncate text-caption text-muted-foreground">
            {chat.description}
          </span>
        )}
        <ChatIdDisclosure id={chat.chat_id} />
      </span>
      {selected && <Check className="mt-1 h-4 w-4 shrink-0 text-foreground" />}
    </div>
  );
}

/** The current selection as an explicit chip, so a restored saved target
 * (raw ID, no resolved name) is as visible as a freshly picked group, and an
 * error in the list never silently drops it. */
export function LarkSelectedChatChip({
  value,
  onClear,
  disabled,
}: {
  value: LarkChatSelection;
  onClear: () => void;
  disabled?: boolean;
}) {
  const { t } = useT("lark");
  const label = value.name.trim() !== "" ? value.name : value.chatId;
  return (
    <div className="flex items-center justify-between gap-2 rounded-md border border-primary/40 bg-primary/5 px-3 py-2">
      <span className="min-w-0">
        <span className="block text-micro text-muted-foreground">
          {t(($) => $.picker.selected_group)}
        </span>
        <span className="block truncate text-body font-medium">{label}</span>
        {value.name.trim() !== "" && <ChatIdDisclosure id={value.chatId} />}
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

/** Shared list body: rows plus the loading / empty / no-results / load-more
 * states. Rendered only once discovery is known to work (supported caps). */
export function LarkChatList({
  list,
  selectedIds,
  onToggle,
  disabled,
  checkbox,
  isRowDisabled,
}: {
  list: ReturnType<typeof useLarkTargetChats>;
  selectedIds: ReadonlySet<string>;
  onToggle: (chat: LarkDiscoveredChat) => void;
  disabled?: boolean;
  /** Multi-select renders rows as checkbox options (aria-checked). */
  checkbox?: boolean;
  /** Extra per-row disable (e.g. the multi-select's conversation cap). */
  isRowDisabled?: (chat: LarkDiscoveredChat) => boolean;
}) {
  const { t } = useT("lark");
  return (
    <div className="space-y-2">
      {list.errorKey != null && (
        <LarkDiscoveryErrorAlert
          errorKey={list.errorKey}
          onRetry={list.retry}
          onRestart={list.restart}
        />
      )}
      <div
        role="listbox"
        aria-multiselectable={checkbox === true || undefined}
        aria-label={t(($) => $.picker.list_label)}
        className="max-h-56 divide-y overflow-y-auto rounded-md border"
      >
        {list.items.map((chat) => {
          const rowDisabled = disabled === true || isRowDisabled?.(chat) === true;
          return checkbox ? (
            <label
              key={chat.chat_id}
              className={cn(
                "flex w-full items-start gap-2.5 px-3 py-2 transition-colors",
                rowDisabled ? "cursor-not-allowed opacity-60" : "cursor-pointer hover:bg-muted",
                selectedIds.has(chat.chat_id) && "bg-muted/60 font-medium",
              )}
            >
              <input
                type="checkbox"
                className="mt-1"
                checked={selectedIds.has(chat.chat_id)}
                disabled={rowDisabled}
                onChange={() => onToggle(chat)}
              />
              <LarkChatAvatar
                name={chat.name.trim() !== "" ? chat.name : t(($) => $.picker.unnamed)}
                avatar={chat.avatar}
              />
              <span className="min-w-0 flex-1">
                <span className="flex items-center gap-1.5">
                  <span className="truncate text-body">
                    {chat.name.trim() !== "" ? chat.name : t(($) => $.picker.unnamed)}
                  </span>
                  {chat.external === true && (
                    <Badge variant="secondary" className="shrink-0">
                      {t(($) => $.picker.external_badge)}
                    </Badge>
                  )}
                </span>
                {chat.description.trim() !== "" && (
                  <span className="block truncate text-caption text-muted-foreground">
                    {chat.description}
                  </span>
                )}
                <ChatIdDisclosure id={chat.chat_id} />
              </span>
            </label>
          ) : (
            <LarkChatRow
              key={chat.chat_id}
              chat={chat}
              selected={selectedIds.has(chat.chat_id)}
              onSelect={() => onToggle(chat)}
              disabled={rowDisabled}
            />
          );
        })}
        {list.isLoading && (
          <p className="px-3 py-4 text-center text-caption text-muted-foreground">
            {t(($) => $.picker.loading)}
          </p>
        )}
        {!list.isLoading && !list.hasMore && list.items.length === 0 && list.errorKey == null && (
          <p className="px-3 py-4 text-center text-caption text-muted-foreground">
            {list.query === ""
              ? t(($) => $.picker.empty)
              : t(($) => $.picker.no_results, { query: list.query })}
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
            : t(($) => $.picker.load_more)}
        </Button>
      )}
    </div>
  );
}

/**
 * Single-select group picker for one bot installation (OL-74). Replaces raw
 * oc_ entry whenever the server supports discovery; when it does not (stub
 * transport, pre-discovery server, or a caller without the management role)
 * the caller-supplied `fallback` (the legacy ID input) renders instead, so
 * every configuration path that worked before keeps working.
 *
 * Selection is committed only by an explicit row pick — the component never
 * auto-selects the first/latest group, and a failed list keeps showing the
 * previously picked chip.
 */
export function LarkChatPicker({
  wsId,
  installationId,
  value,
  onChange,
  disabled,
  fallback,
  enabled = true,
}: {
  wsId: string;
  installationId: string;
  value: LarkChatSelection | null;
  onChange: (selection: LarkChatSelection | null) => void;
  disabled?: boolean;
  /** Rendered (with a note) whenever discovery is unavailable. */
  fallback: React.ReactNode;
  enabled?: boolean;
}) {
  const { t } = useT("lark");
  const caps = useLarkTargetCapabilities(wsId, installationId, { enabled });
  const supported = caps.data?.chat_list_supported === true;
  const list = useLarkTargetChats(wsId, installationId, { enabled: enabled && supported });

  if (caps.isPending && enabled) {
    return (
      <p className="py-2 text-caption text-muted-foreground">
        {t(($) => $.picker.loading)}
      </p>
    );
  }
  if (!supported) {
    // Forbidden is NOT a fallback case (OL-72 contract): the picker explains
    // the management role required and never offers raw-ID entry as a bypass.
    // The already-saved target stays visible and keeps its existing save
    // semantics.
    if (caps.isError && larkDiscoveryErrorKey(caps.error) === "forbidden") {
      return (
        <div className="space-y-2">
          {value != null && (
            <LarkSelectedChatChip value={value} onClear={() => onChange(null)} disabled={disabled} />
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
    list.query === "" &&
    !list.items.some((c) => c.chat_id === value.chatId);

  return (
    <div className="space-y-2">
      {value != null && (
        <LarkSelectedChatChip
          value={value}
          onClear={() => onChange(null)}
          disabled={disabled}
        />
      )}
      <div className="relative">
        <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={list.search}
          onChange={(e) => list.setSearch(e.target.value)}
          placeholder={t(($) => $.picker.search_placeholder)}
          aria-label={t(($) => $.picker.search_label)}
          className="pl-8"
          disabled={disabled}
        />
      </div>
      <LarkChatList
        list={list}
        selectedIds={value != null ? new Set([value.chatId]) : new Set<string>()}
        onToggle={(chat) =>
          onChange(
            value?.chatId === chat.chat_id ? null : { chatId: chat.chat_id, name: chat.name },
          )
        }
        disabled={disabled}
      />
      {savedMissing && (
        <p role="status" className="text-caption text-warning">
          {t(($) => $.picker.saved_group_missing)}
        </p>
      )}
    </div>
  );
}
