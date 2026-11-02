"use client";

import { useState } from "react";
import { Plus, Search } from "@multica/ui/icons";
import type { LarkDiscoveredChat } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../i18n";
import { larkDiscoveryErrorKey } from "./discovery";
import { useLarkTargetCapabilities, useLarkTargetChats } from "./use-lark-discovery";
import { LarkSelectionChips } from "./picker-common";
import { LarkChatList } from "./lark-chat-picker";
import type { LarkChatSelection } from "./picker-common";

/**
 * Multi-select group picker for the conversation-grant form (OL-74): the
 * authorized groups are chips (removed individually), new groups come from
 * an expandable browse panel that reuses the shared joined-group query with
 * name search and paging. Selection is deduplicated by chat_id and capped so
 * the (groups + direct chats) total can never exceed the server's 50-chat
 * grant limit. Raw oc_ typing remains as the caller-supplied `fallback` for
 * servers/transports without discovery.
 */
export function LarkChatMultiSelect({
  wsId,
  installationId,
  selected,
  onChange,
  max,
  otherCount,
  disabled,
  fallback,
}: {
  wsId: string;
  installationId: string;
  selected: LarkChatSelection[];
  onChange: (next: LarkChatSelection[]) => void;
  /** Total conversation cap across groups and direct chats. */
  max: number;
  /** Conversations configured outside this list (direct chats textarea). */
  otherCount: number;
  disabled?: boolean;
  fallback: React.ReactNode;
}) {
  const { t } = useT("lark");
  const [expanded, setExpanded] = useState(false);
  const caps = useLarkTargetCapabilities(wsId, installationId);
  const supported = caps.data?.chat_list_supported === true;
  const list = useLarkTargetChats(wsId, installationId, { enabled: supported && expanded });

  const selectedIds = new Set(selected.map((s) => s.chatId));
  const atCap = selected.length + otherCount >= max;

  const toggle = (chat: LarkDiscoveredChat) => {
    if (selectedIds.has(chat.chat_id)) {
      onChange(selected.filter((s) => s.chatId !== chat.chat_id));
      return;
    }
    if (atCap) return;
    onChange([...selected, { chatId: chat.chat_id, name: chat.name }]);
  };

  const chips = (
    <LarkSelectionChips
      selected={selected}
      onRemove={(chatId) => onChange(selected.filter((s) => s.chatId !== chatId))}
      disabled={disabled}
    />
  );

  if (!supported) {
    // Forbidden is NOT a fallback case (OL-72 contract): explain the required
    // management role, never offer raw-ID entry as a bypass. Saved grants
    // stay visible (and removable) — they keep their existing save semantics.
    if (caps.isError && larkDiscoveryErrorKey(caps.error) === "forbidden") {
      return (
        <div className="space-y-2">
          {selected.length > 0 && chips}
          <p className="text-caption text-muted-foreground">
            {t(($) => $.picker.error.forbidden)}
          </p>
        </div>
      );
    }
    return (
      <div className="space-y-2">
        {selected.length > 0 && chips}
        {caps.isPending ? (
          <p className="py-1 text-caption text-muted-foreground">
            {t(($) => $.picker.loading)}
          </p>
        ) : (
          <>
            <p className="text-caption text-muted-foreground">
              {caps.isError
                ? t(($) => $.picker.error[larkDiscoveryErrorKey(caps.error)])
                : t(($) => $.picker.error.unsupported)}
            </p>
            {fallback}
          </>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-2">
      {selected.length > 0 && chips}
      <p className="text-micro text-muted-foreground">
        {t(($) => $.picker.multi_selected, { count: selected.length + otherCount, max })}
      </p>
      {!expanded ? (
        <Button
          size="sm"
          variant="outline"
          onClick={() => setExpanded(true)}
          disabled={disabled || atCap}
        >
          <Plus className="h-3 w-3" />
          {t(($) => $.picker.multi_add)}
        </Button>
      ) : (
        <div className="space-y-2 rounded-md border p-2">
          <div className="relative">
            <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={list.search}
              onChange={(e) => list.setSearch(e.target.value)}
              placeholder={t(($) => $.picker.search_placeholder)}
              aria-label={t(($) => $.picker.search_label)}
              className="pl-8"
              disabled={disabled}
              autoFocus
            />
          </div>
          {atCap && (
            <p role="status" className="text-caption text-warning">
              {t(($) => $.picker.multi_max, { max })}
            </p>
          )}
          <LarkChatList
            list={list}
            selectedIds={selectedIds}
            onToggle={toggle}
            disabled={disabled}
            isRowDisabled={(chat) => atCap && !selectedIds.has(chat.chat_id)}
            checkbox
          />
          <div className="flex justify-end">
            <Button size="sm" variant="ghost" onClick={() => setExpanded(false)}>
              {t(($) => $.picker.multi_done)}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
