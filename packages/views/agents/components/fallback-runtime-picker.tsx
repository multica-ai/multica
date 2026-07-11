"use client";

import { useMemo, useState } from "react";
import { ChevronDown, ChevronUp, Plus, Trash2 } from "lucide-react";
import type { MemberWithUser, RuntimeDevice } from "@multica/core/types";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { ProviderLogo } from "../../runtimes/components/provider-logo";

type RuntimeFilter = "mine" | "all";

interface FallbackRuntimesInputProps {
  runtimes: RuntimeDevice[];
  members: MemberWithUser[];
  currentUserId: string | null;
  primaryRuntimeId: string;
  value: string[];
  onChange: (runtimeIds: string[]) => void;
  disabled?: boolean;
}

export function FallbackRuntimesInput({
  runtimes,
  members,
  currentUserId,
  primaryRuntimeId,
  value,
  onChange,
  disabled = false,
}: FallbackRuntimesInputProps) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState<RuntimeFilter>("mine");

  const normalizedValue = useMemo(
    () => value.filter((id, index, ids) => id && id !== primaryRuntimeId && ids.indexOf(id) === index),
    [primaryRuntimeId, value],
  );

  const selected = useMemo(
    () =>
      normalizedValue
        .map((runtimeId) => runtimes.find((runtime) => runtime.id === runtimeId))
        .filter((runtime): runtime is RuntimeDevice => Boolean(runtime)),
    [normalizedValue, runtimes],
  );

  const selectable = useMemo(() => {
    const excluded = new Set([primaryRuntimeId, ...normalizedValue]);
    return runtimes.filter((runtime) => {
      if (excluded.has(runtime.id)) return false;
      if (filter === "mine" && currentUserId && runtime.owner_id !== currentUserId) return false;
      return true;
    });
  }, [currentUserId, filter, normalizedValue, primaryRuntimeId, runtimes]);

  const hasSharedRuntimes = runtimes.some((runtime) => runtime.owner_id !== currentUserId);

  const memberForRuntime = (runtime: RuntimeDevice) =>
    runtime.owner_id ? members.find((member) => member.user_id === runtime.owner_id) : null;

  const move = (fromIndex: number, toIndex: number) => {
    const next = [...normalizedValue];
    const [moved] = next.splice(fromIndex, 1);
    if (!moved) return;
    next.splice(toIndex, 0, moved);
    onChange(next);
  };

  const remove = (indexToRemove: number) => {
    onChange(normalizedValue.filter((_, index) => index !== indexToRemove));
  };

  const add = (runtimeId: string) => {
    onChange([...normalizedValue, runtimeId]);
    setOpen(false);
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-3">
        <Label className="text-xs text-muted-foreground">
          {t(($) => $.fallback_runtime_picker.label)}
        </Label>
        {hasSharedRuntimes && (
          <div className="flex rounded-md bg-muted p-0.5 text-xs">
            <button
              type="button"
              onClick={() => setFilter("mine")}
              className={`rounded px-2 py-0.5 ${filter === "mine" ? "bg-background shadow-sm" : "text-muted-foreground"}`}
            >
              {t(($) => $.fallback_runtime_picker.mine)}
            </button>
            <button
              type="button"
              onClick={() => setFilter("all")}
              className={`rounded px-2 py-0.5 ${filter === "all" ? "bg-background shadow-sm" : "text-muted-foreground"}`}
            >
              {t(($) => $.fallback_runtime_picker.all)}
            </button>
          </div>
        )}
      </div>

      {selected.length > 0 && (
        <div className="space-y-1.5">
          {selected.map((runtime, index) => {
            const owner = memberForRuntime(runtime);
            return (
              <div
                key={runtime.id}
                className="flex items-center gap-2 rounded-md border bg-background px-2 py-2"
              >
                <ProviderLogo provider={runtime.provider} className="h-4 w-4 shrink-0" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{runtime.name}</div>
                  <div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
                    {owner && <ActorAvatar actorType="member" actorId={owner.user_id} size="xs" />}
                    <span className="truncate">{owner?.name ?? runtime.device_info}</span>
                  </div>
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7"
                  onClick={() => move(index, index - 1)}
                  disabled={disabled || index === 0}
                >
                  <ChevronUp className="h-3.5 w-3.5" />
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7"
                  onClick={() => move(index, index + 1)}
                  disabled={disabled || index === selected.length - 1}
                >
                  <ChevronDown className="h-3.5 w-3.5" />
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7 text-destructive"
                  onClick={() => remove(index)}
                  disabled={disabled}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            );
          })}
        </div>
      )}

      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          className={buttonVariants({
            variant: "outline",
            className: "w-full justify-start gap-2",
          })}
          disabled={disabled || selectable.length === 0}
        >
          <Plus className="h-4 w-4" />
          {t(($) => $.fallback_runtime_picker.add)}
        </PopoverTrigger>
        <PopoverContent align="start" className="max-h-64 w-[var(--anchor-width)] overflow-y-auto p-1">
          {selectable.length === 0 ? (
            <div className="px-2 py-2 text-sm text-muted-foreground">
              {t(($) => $.fallback_runtime_picker.empty)}
            </div>
          ) : (
            selectable.map((runtime) => {
              const owner = memberForRuntime(runtime);
              return (
                <button
                  key={runtime.id}
                  type="button"
                  onClick={() => add(runtime.id)}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-sm hover:bg-accent"
                >
                  <ProviderLogo provider={runtime.provider} className="h-4 w-4 shrink-0" />
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-medium">{runtime.name}</div>
                    <div className="truncate text-xs text-muted-foreground">
                      {owner?.name ?? runtime.device_info}
                    </div>
                  </div>
                  <span
                    className={`h-2 w-2 shrink-0 rounded-full ${
                      runtime.status === "online" ? "bg-success" : "bg-muted-foreground/40"
                    }`}
                  />
                </button>
              );
            })
          )}
        </PopoverContent>
      </Popover>
    </div>
  );
}
