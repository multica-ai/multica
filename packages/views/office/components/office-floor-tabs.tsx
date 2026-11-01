"use client";

import { useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { toast } from "sonner";
import { Ellipsis, Plus } from "@multica/ui/icons";
import {
  OFFICE_FLOOR_NAME_MAX_LENGTH,
  OFFICE_MAX_FLOORS,
  useCreateOfficeFloor,
  useDeleteOfficeFloor,
  useUpdateOfficeFloor,
  type OfficeFloor,
} from "@multica/core/office";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { PAGE_GUTTER } from "../../layout/page-header";
import { useT } from "../../i18n";

/**
 * The office's floors as tabs. For the owner or an admin a double-click renames
 * a floor in place, a menu moves or deletes it, and the trailing `+` adds one.
 * Everyone else only switches. A dot marks a floor with a proposal waiting.
 */
export function OfficeFloorTabs({
  floors,
  currentId,
  occupied,
  attention,
  canManage,
  onSelect,
  children,
}: {
  floors: readonly OfficeFloor[];
  currentId: string | null;
  /** Floors that still hold a live squad; only an empty floor can be deleted. */
  occupied: ReadonlySet<string>;
  /** Floors with a pending proposal. */
  attention?: ReadonlySet<string>;
  canManage: boolean;
  onSelect: (floorId: string) => void;
  /** Trailing controls, aligned to the end of the strip. */
  children?: ReactNode;
}) {
  const { t } = useT("issues");
  const create = useCreateOfficeFloor();
  const update = useUpdateOfficeFloor();
  const remove = useDeleteOfficeFloor();
  const [renamingId, setRenamingId] = useState<string | null>(null);

  const addFloor = () => {
    const used = new Set(floors.map((floor) => floor.name));
    let n = floors.length + 1;
    while (used.has(t(($) => $.office.floor_default_name, { n }))) n += 1;
    create.mutate(
      { name: t(($) => $.office.floor_default_name, { n }) },
      {
        onSuccess: (floor) => onSelect(floor.id),
        onError: () => toast.error(t(($) => $.office.floor_add_failed)),
      },
    );
  };

  const rename = (floor: OfficeFloor, raw: string) => {
    setRenamingId(null);
    const name = raw.trim();
    if (!name || name === floor.name) return;
    update.mutate({ id: floor.id, name }, { onError: () => toast.error(t(($) => $.office.floor_rename_failed)) });
  };

  const move = (floor: OfficeFloor, position: number) =>
    update.mutate({ id: floor.id, position }, { onError: () => toast.error(t(($) => $.office.floor_move_failed)) });

  const deleteFloor = (floor: OfficeFloor) =>
    remove.mutate(floor.id, {
      onSuccess: () => {
        if (floor.id === currentId) {
          const next = floors.find((other) => other.id !== floor.id);
          if (next) onSelect(next.id);
        }
      },
      onError: () => toast.error(t(($) => $.office.floor_delete_failed)),
    });

  return (
    <div className={cn("flex min-h-9 shrink-0 items-center gap-1 pb-2", PAGE_GUTTER)}>
      <div role="tablist" aria-label={t(($) => $.office.floors)} className="flex min-w-0 items-center gap-1 overflow-x-auto">
        {floors.map((floor, index) => {
          const selected = floor.id === currentId;
          return (
            <div key={floor.id} className="flex shrink-0 items-center">
              {renamingId === floor.id ? (
                <RenameField initial={floor.name} label={t(($) => $.office.floor_rename)} onDone={(name) => rename(floor, name)} />
              ) : (
                <button
                  type="button"
                  role="tab"
                  aria-selected={selected}
                  onClick={() => onSelect(floor.id)}
                  onDoubleClick={canManage ? () => setRenamingId(floor.id) : undefined}
                  className={cn(
                    "flex h-7 items-center gap-1.5 rounded-md px-2.5 text-body outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring",
                    selected ? "bg-accent font-medium text-accent-foreground" : "text-muted-foreground hover:bg-accent/50",
                  )}
                >
                  {floor.name}
                  {attention?.has(floor.id) ? (
                    <span aria-label={t(($) => $.office.floor_attention)} role="img" className="size-1.5 rounded-full bg-brand" />
                  ) : null}
                </button>
              )}
              {canManage && selected && renamingId !== floor.id ? (
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-xs"
                        aria-label={t(($) => $.office.floor_menu, { name: floor.name })}
                      />
                    }
                  >
                    <Ellipsis className="size-3.5" />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start" className="w-44">
                    <DropdownMenuItem onClick={() => setRenamingId(floor.id)}>{t(($) => $.office.floor_rename)}</DropdownMenuItem>
                    <DropdownMenuItem disabled={index === 0} onClick={() => move(floor, index - 1)}>
                      {t(($) => $.office.floor_move_left)}
                    </DropdownMenuItem>
                    <DropdownMenuItem disabled={index === floors.length - 1} onClick={() => move(floor, index + 1)}>
                      {t(($) => $.office.floor_move_right)}
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem variant="destructive" disabled={occupied.has(floor.id)} onClick={() => deleteFloor(floor)}>
                      {t(($) => $.office.floor_delete)}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              ) : null}
            </div>
          );
        })}
        {canManage ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={t(($) => $.office.floor_add)}
            disabled={floors.length >= OFFICE_MAX_FLOORS || create.isPending}
            onClick={addFloor}
          >
            <Plus className="size-4" />
          </Button>
        ) : null}
      </div>
      <div className="ml-auto flex shrink-0 items-center gap-1">{children}</div>
    </div>
  );
}

/** A tab's name as a text field: Enter or leaving it saves, Escape keeps the old name. */
function RenameField({ initial, label, onDone }: { initial: string; label: string; onDone: (name: string) => void }) {
  const cancelled = useRef(false);
  const finish = (value: string) => onDone(cancelled.current ? initial : value);
  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter") event.currentTarget.blur();
    if (event.key === "Escape") {
      cancelled.current = true;
      event.currentTarget.blur();
    }
  };
  return (
    <Input
      autoFocus
      aria-label={label}
      defaultValue={initial}
      maxLength={OFFICE_FLOOR_NAME_MAX_LENGTH}
      className="h-7 w-36"
      onFocus={(event) => event.currentTarget.select()}
      onKeyDown={onKeyDown}
      onBlur={(event) => finish(event.currentTarget.value)}
    />
  );
}
