"use client";

import { useMemo, useState } from "react";
import { Cloud, Monitor } from "lucide-react";
import type { AgentRuntime, MemberWithUser } from "@multica/core/types";
import { ActorAvatar } from "../../../common/actor-avatar";
import {
  PickerItem,
  PropertyPicker,
} from "../../../issues/components/pickers";
import { ProviderLogo } from "../../../runtimes/components/provider-logo";
import { CHIP_CLASS } from "./chip";

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
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState<Filter>("mine");

  const selected = runtimes.find((r) => r.id === value) ?? null;
  const Icon = selected?.runtime_mode === "cloud" ? Cloud : Monitor;

  if (!canEdit) {
    const isOnline = selected?.status === "online";
    return (
      <span className="inline-flex min-w-0 items-center gap-1.5 px-1.5 py-0.5 text-xs text-muted-foreground">
        <Icon className="h-3 w-3 shrink-0" />
        <span className="min-w-0 truncate font-mono">
          {selected?.name ?? "No runtime"}
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
  // device_info still shows on each row in the popover (small mono line),
  // which is the right place for system detail.
  const triggerLabel = selected?.name ?? "No runtime";
  const isOnline = selected?.status === "online";
  const triggerTitle = selected
    ? `Runtime · ${selected.name} · ${isOnline ? "online" : "offline"}`
    : "Runtime · none selected";

  const hasOtherRuntimes = runtimes.some((r) => r.owner_id !== currentUserId);

  const filtered = useMemo(() => {
    const list =
      filter === "mine" && currentUserId
        ? runtimes.filter((r) => r.owner_id === currentUserId)
        : runtimes;
    return [...list].sort((a, b) => {
      if (a.owner_id === currentUserId && b.owner_id !== currentUserId)
        return -1;
      if (a.owner_id !== currentUserId && b.owner_id === currentUserId)
        return 1;
      return 0;
    });
  }, [runtimes, filter, currentUserId]);

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
                Mine
              </FilterButton>
              <FilterButton
                active={filter === "all"}
                onClick={() => setFilter("all")}
              >
                All
              </FilterButton>
            </div>
          </div>
        ) : undefined
      }
    >
      {filtered.length === 0 ? (
        <p className="px-2 py-3 text-center text-xs text-muted-foreground">
          No runtimes
        </p>
      ) : (
        filtered.map((rt) => {
          const owner = getOwner(rt.owner_id);
          const rtOnline = rt.status === "online";
          // Tooltip echoes the chip's structure: name + owner + status. Skip
          // device_info because rt.name already embeds the host (it'd just
          // repeat), and the row visually shows device_info on its second
          // line anyway for users who do need that detail.
          const tooltip = [
            rt.name,
            owner ? `owned by ${owner.name}` : null,
            rtOnline ? "online" : "offline",
          ]
            .filter(Boolean)
            .join(" · ");
          return (
            <PickerItem
              key={rt.id}
              selected={rt.id === value}
              onClick={() => void select(rt.id)}
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
                      Cloud
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
                aria-label={rtOnline ? "online" : "offline"}
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
