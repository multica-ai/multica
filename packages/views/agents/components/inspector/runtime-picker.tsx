"use client";

import { useMemo, useState } from "react";
import { Cloud, Lock, Monitor } from "lucide-react";
import type { AgentRuntime, MemberWithUser } from "@multica/core/types";
import { ActorAvatar } from "../../../common/actor-avatar";
import {
  PickerItem,
  PropertyPicker,
} from "../../../issues/components/pickers";
import { ProviderLogo } from "../../../runtimes/components/provider-logo";
import { CHIP_CLASS } from "./chip";
import { useT } from "../../../i18n";

type Filter = "mine" | "all";

/**
 * Inline runtime picker for the agent inspector. Mirrors the runtime selector
 * the previous Settings tab embedded — same Mine/All filter, same provider
 * logos, same online dot — but renders inside the inspector's PropRow so
 * users don't have to leave the page to switch runtime.
 */
export function RuntimePicker({
  value,
  runtimes,
  members,
  currentUserId,
  canEdit = true,
  onChange,
}: {
  value: string;
  runtimes: AgentRuntime[];
  members: MemberWithUser[];
  currentUserId: string | null;
  /** When false, render a static read-only display and skip the popover. */
  canEdit?: boolean;
  onChange: (runtimeId: string) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState<Filter>("mine");

  const selected = runtimes.find((r) => r.id === value) ?? null;
  const Icon = selected?.runtime_mode === "cloud" ? Cloud : Monitor;

  // Compute filtered list unconditionally — the early `!canEdit` return
  // below would otherwise re-order this hook across renders.
  const isDisabled = (r: AgentRuntime): boolean => {
    if (!currentUserId) return false;
    if (r.owner_id === currentUserId) return false;
    return r.visibility !== "public";
  };
  const filtered = useMemo(() => {
    const list =
      filter === "mine" && currentUserId
        ? runtimes.filter((r) => r.owner_id === currentUserId)
        : runtimes;
    return [...list].sort((a, b) => {
      const aMine = a.owner_id === currentUserId;
      const bMine = b.owner_id === currentUserId;
      if (aMine && !bMine) return -1;
      if (!aMine && bMine) return 1;
      const aDisabled = isDisabled(a);
      const bDisabled = isDisabled(b);
      if (!aDisabled && bDisabled) return -1;
      if (aDisabled && !bDisabled) return 1;
      return 0;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runtimes, filter, currentUserId]);

  if (!canEdit) {
    const isOnline = selected?.status === "online";
    return (
      <span className="inline-flex min-w-0 items-center gap-1.5 px-1.5 py-0.5 text-xs text-muted-foreground">
        <Icon className="h-3 w-3 shrink-0" />
        <span className="min-w-0 truncate font-mono">
          {selected?.name ?? t(($) => $.pickers.runtime_none)}
        </span>
        {selected && (
          <span
            className={`ml-auto h-1.5 w-1.5 shrink-0 rounded-full ${
              isOnline ? "bg-success" : "bg-muted-foreground/40"
            }`}
          />
        )}
      </span>
    );
  }
  // The chip shows only the runtime name. `runtime.name` already comes back
  // from the back-end pre-formatted as e.g. "Claude (host.local)", so we
  // deliberately do NOT append `device_info` to the tooltip — that string
  // also leads with the host and would just repeat what's already in name,
  // producing the "Claude (host) (host · 2.1.121 (Claude Code))" mess.
  const triggerLabel = selected?.name ?? t(($) => $.pickers.runtime_none);
  const isOnline = selected?.status === "online";
  const triggerTitle = selected
    ? t(($) => $.pickers.runtime_tooltip, {
        name: selected.name,
        status: isOnline ? t(($) => $.pickers.runtime_online) : t(($) => $.pickers.runtime_offline),
      })
    : t(($) => $.pickers.runtime_tooltip_none);

  const hasOtherRuntimes = runtimes.some((r) => r.owner_id !== currentUserId);

  const getOwner = (id: string | null) =>
    id ? members.find((m) => m.user_id === id) ?? null : null;

  const select = async (id: string) => {
    setOpen(false);
    if (id !== value) await onChange(id);
  };

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-auto min-w-[18rem] max-w-md"
      align="start"
      tooltip={triggerTitle}
      triggerRender={
        <button
          type="button"
          className={CHIP_CLASS}
          aria-label={triggerTitle}
        />
      }
      trigger={
        <>
          <Icon className="h-3 w-3 shrink-0 text-muted-foreground" />
          <span className="min-w-0 truncate font-mono">{triggerLabel}</span>
          {selected && (
            <span
              className={`ml-auto h-1.5 w-1.5 shrink-0 rounded-full ${
                isOnline ? "bg-success" : "bg-muted-foreground/40"
              }`}
            />
          )}
        </>
      }
      header={
        hasOtherRuntimes ? (
          <div className="p-2">
            <div className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
              <FilterButton
                active={filter === "mine"}
                onClick={() => setFilter("mine")}
              >
                {t(($) => $.scope.mine)}
              </FilterButton>
              <FilterButton
                active={filter === "all"}
                onClick={() => setFilter("all")}
              >
                {t(($) => $.scope.all)}
              </FilterButton>
            </div>
          </div>
        ) : undefined
      }
    >
      {filtered.length === 0 ? (
        <p className="px-2 py-3 text-center text-xs text-muted-foreground">
          {t(($) => $.pickers.runtime_empty)}
        </p>
      ) : (
        filtered.map((rt) => {
          const owner = getOwner(rt.owner_id);
          const rtOnline = rt.status === "online";
          const locked = isDisabled(rt);
          const tooltip = [
            rt.name,
            owner ? t(($) => $.pickers.runtime_owned_by, { name: owner.name }) : null,
            rtOnline ? t(($) => $.pickers.runtime_online) : t(($) => $.pickers.runtime_offline),
            locked ? t(($) => $.create_dialog.runtime_private_locked_tooltip) : null,
          ]
            .filter(Boolean)
            .join(" · ");
          return (
            <PickerItem
              key={rt.id}
              selected={rt.id === value}
              disabled={locked}
              onClick={() => {
                if (locked) return;
                void select(rt.id);
              }}
              tooltip={tooltip}
            >
              <ProviderLogo
                provider={rt.provider}
                className="h-4 w-4 shrink-0"
              />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5">
                  <span className="truncate text-sm font-medium">
                    {rt.name}
                  </span>
                  {rt.runtime_mode === "cloud" && (
                    <span className="shrink-0 rounded bg-info/10 px-1 text-[10px] font-medium text-info">
                      {t(($) => $.create_dialog.runtime_cloud_badge)}
                    </span>
                  )}
                  {locked && (
                    <span className="shrink-0 inline-flex items-center gap-0.5 rounded bg-muted px-1 text-[10px] font-medium text-muted-foreground">
                      <Lock className="h-2.5 w-2.5" />
                      {t(($) => $.create_dialog.runtime_private_badge)}
                    </span>
                  )}
                </div>
                <div className="mt-0.5 flex items-center gap-1.5 text-xs text-muted-foreground">
                  {owner && (
                    <span className="flex min-w-0 items-center gap-1">
                      <ActorAvatar
                        actorType="member"
                        actorId={owner.user_id}
                        size={12}
                      />
                      <span className="truncate">{owner.name}</span>
                    </span>
                  )}
                  {owner && rt.device_info && (
                    <span className="text-muted-foreground/40">·</span>
                  )}
                  {rt.device_info && (
                    <span className="truncate font-mono text-[10px]">
                      {rt.device_info}
                    </span>
                  )}
                </div>
              </div>
              <span
                className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                  rtOnline ? "bg-success" : "bg-muted-foreground/40"
                }`}
                aria-label={rtOnline ? t(($) => $.pickers.runtime_online) : t(($) => $.pickers.runtime_offline)}
              />
            </PickerItem>
          );
        })
      )}
    </PropertyPicker>
  );
}

function FilterButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex-1 rounded px-2 py-0.5 text-xs font-medium transition-colors ${
        active
          ? "bg-background text-foreground shadow-sm"
          : "text-muted-foreground hover:text-foreground"
      }`}
    >
      {children}
    </button>
  );
}
