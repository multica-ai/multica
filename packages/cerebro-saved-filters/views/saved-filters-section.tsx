"use client";

import { useEffect, useMemo, useState } from "react";
import { Bookmark, Check, Globe, Lock, Plus, Share2, Trash2, Users } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";

import { useWorkspaceId } from "@multica/core/hooks";
import { useViewStore, useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import { memberListOptions } from "@multica/core/workspace/queries";
import { groupListOptions } from "@multica/cerebro-groups";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { RadioGroup, RadioGroupItem } from "@multica/ui/components/ui/radio-group";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";
import {
  Avatar,
  AvatarFallback,
  AvatarImage,
} from "@multica/ui/components/ui/avatar";
import { Label } from "@multica/ui/components/ui/label";

import {
  useCreateSavedFilter,
  useDeleteSavedFilter,
  useSavedFilter,
  useSavedFilters,
  useUpdateSavedFilter,
  useCanShareFilters,
} from "../core/queries";
import {
  extractFilterSnapshot,
  isSnapshotEmpty,
  snapshotToState,
  snapshotsEqual,
} from "../core/filter-snapshot";
import { useSaveFilterDialogStore } from "./save-dialog-store";
import type {
  SavedFilter,
  SavedFilterShareTarget,
  SavedFilterVisibility,
} from "../core/types";

const DEFAULT_SURFACE = "issues";

// Icon hinting a filter's sharing reach, shown next to owned filters.
function VisibilityIcon({ visibility }: { visibility: SavedFilterVisibility }) {
  if (visibility === "workspace") {
    return <Globe className="size-3.5 text-muted-foreground" aria-label="Whole team" />;
  }
  if (visibility === "shared") {
    return <Users className="size-3.5 text-muted-foreground" aria-label="Shared" />;
  }
  return <Lock className="size-3.5 text-muted-foreground" aria-label="Only you" />;
}

/**
 * SavedFiltersMenuSection renders at the top of the issue Filter dropdown: the
 * member's own saved filters (apply / re-share / delete) plus any filters shared
 * with them, and a "Save current filter…" action. Hidden when the
 * cerebro_saved_filters flag is off. Render it INSIDE the Filter
 * DropdownMenuContent; pair it with <SaveFilterDialogHost /> mounted outside.
 */
export function SavedFiltersMenuSection() {
  const enabled = useFeatureFlag("cerebro_saved_filters");
  const wsId = useWorkspaceId();
  const storeApi = useViewStoreApi();
  // Subscribe so the active-filter highlight re-evaluates as the user edits.
  const liveStatus = useViewStore((s) => s.statusFilters);
  const { data: filters = [] } = useSavedFilters(wsId, undefined);
  const requestSave = useSaveFilterDialogStore((s) => s.requestSave);
  const requestEdit = useSaveFilterDialogStore((s) => s.requestEdit);
  const del = useDeleteSavedFilter(wsId);

  const owned = useMemo(() => filters.filter((f) => f.isOwner), [filters]);
  const shared = useMemo(() => filters.filter((f) => !f.isOwner), [filters]);

  if (!enabled) return null;
  void liveStatus; // referenced only to keep the subscription active

  const current = extractFilterSnapshot(storeApi.getState());

  const onSave = () => {
    const snapshot = extractFilterSnapshot(storeApi.getState());
    if (isSnapshotEmpty(snapshot)) {
      toast.info("Add at least one filter before saving");
      return;
    }
    requestSave(snapshot);
  };

  const renderApply = (f: SavedFilter, opts?: { owned?: boolean }) => {
    const active = snapshotsEqual(current, f.filterState);
    return (
      <DropdownMenuItem
        key={f.id}
        className="group flex items-center gap-2"
        onClick={() => storeApi.setState(snapshotToState(f.filterState))}
      >
        <Check className={active ? "size-3.5 text-primary" : "size-3.5 opacity-0"} />
        <span className="flex-1 truncate">{f.name}</span>
        {opts?.owned ? (
          <>
            <VisibilityIcon visibility={f.visibility} />
            <span
              role="button"
              tabIndex={-1}
              aria-label={`Share ${f.name}`}
              className="ml-1 hidden rounded-sm p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground group-hover:inline-flex"
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                requestEdit(f.id);
              }}
              onPointerDown={(e) => e.stopPropagation()}
            >
              <Share2 className="size-3.5" />
            </span>
            <span
              role="button"
              tabIndex={-1}
              aria-label={`Delete ${f.name}`}
              className="hidden rounded-sm p-0.5 text-muted-foreground hover:bg-destructive/10 hover:text-destructive group-hover:inline-flex"
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                del.mutate(f.id, {
                  onError: () => toast.error("Failed to delete filter"),
                });
              }}
              onPointerDown={(e) => e.stopPropagation()}
            >
              <Trash2 className="size-3.5" />
            </span>
          </>
        ) : (
          f.ownerName && (
            <span className="ml-1 max-w-24 truncate text-xs text-muted-foreground">
              {f.ownerName}
            </span>
          )
        )}
      </DropdownMenuItem>
    );
  };

  return (
    <>
      <DropdownMenuGroup>
        <DropdownMenuLabel className="flex items-center gap-1.5 text-muted-foreground">
          <Bookmark className="size-3.5" />
          Saved filters
        </DropdownMenuLabel>

        {owned.length === 0 ? (
          <DropdownMenuItem disabled className="text-xs text-muted-foreground">
            No saved filters yet
          </DropdownMenuItem>
        ) : (
          owned.map((f) => renderApply(f, { owned: true }))
        )}

        <DropdownMenuItem onClick={onSave}>
          <Plus className="size-3.5" />
          <span>Save current filter…</span>
        </DropdownMenuItem>
      </DropdownMenuGroup>

      {shared.length > 0 && (
        <DropdownMenuGroup>
          <DropdownMenuLabel className="flex items-center gap-1.5 text-muted-foreground">
            <Users className="size-3.5" />
            Shared with you
          </DropdownMenuLabel>
          {shared.map((f) => renderApply(f))}
        </DropdownMenuGroup>
      )}

      <DropdownMenuSeparator />
    </>
  );
}

// shareKey / parseShareKey bridge the Set<string> selection state used by the
// picker and the {targetType,targetId} wire shape.
function shareKey(t: SavedFilterShareTarget): string {
  return `${t.targetType}:${t.targetId}`;
}

/**
 * SaveFilterDialogHost owns the naming + sharing dialog. It lives OUTSIDE the
 * Filter dropdown so it survives the dropdown closing. Mount it once, next to
 * the Filter dropdown. Handles both creating a new filter (from the current
 * snapshot) and re-sharing an existing owned filter.
 */
export function SaveFilterDialogHost({ surface }: { surface?: string }) {
  const enabled = useFeatureFlag("cerebro_saved_filters");
  const wsId = useWorkspaceId();
  const open = useSaveFilterDialogStore((s) => s.open);
  const mode = useSaveFilterDialogStore((s) => s.mode);
  const snapshot = useSaveFilterDialogStore((s) => s.snapshot);
  const filterId = useSaveFilterDialogStore((s) => s.filterId);
  const close = useSaveFilterDialogStore((s) => s.close);

  const create = useCreateSavedFilter(wsId);
  const update = useUpdateSavedFilter(wsId);
  const { data: canShare = false } = useCanShareFilters(wsId);
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId && open,
  });
  const { data: groups = [] } = useQuery({
    ...groupListOptions(wsId),
    enabled: !!wsId && open,
  });
  const editing = useSavedFilter(wsId, mode === "edit" ? filterId : null);

  const [name, setName] = useState("");
  const [visibility, setVisibility] = useState<SavedFilterVisibility>("private");
  const [selected, setSelected] = useState<Set<string>>(new Set());

  // Prefill from the existing filter when re-sharing.
  useEffect(() => {
    if (!open) return;
    if (mode === "edit" && editing.data) {
      setName(editing.data.name);
      setVisibility(editing.data.visibility);
      setSelected(new Set((editing.data.shares ?? []).map(shareKey)));
    }
  }, [open, mode, editing.data]);

  if (!enabled) return null;

  const reset = () => {
    setName("");
    setVisibility("private");
    setSelected(new Set());
  };

  const toggle = (t: SavedFilterShareTarget) => {
    const key = shareKey(t);
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const sharesPayload = (): SavedFilterShareTarget[] => {
    if (visibility !== "shared") return [];
    const out: SavedFilterShareTarget[] = [];
    for (const key of selected) {
      const idx = key.indexOf(":");
      if (idx < 0) continue;
      const targetType = key.slice(0, idx);
      const targetId = key.slice(idx + 1);
      if ((targetType === "group" || targetType === "member") && targetId) {
        out.push({ targetType, targetId });
      }
    }
    return out;
  };

  const submit = () => {
    const trimmed = name.trim();
    if (!trimmed) return;
    const shares = sharesPayload();
    if (mode === "create") {
      if (!snapshot) return;
      create.mutate(
        {
          name: trimmed,
          surface: surface ?? DEFAULT_SURFACE,
          filterState: snapshot,
          visibility,
          shares,
        },
        {
          onSuccess: () => {
            toast.success("Filter saved");
            reset();
            close();
          },
          onError: () => toast.error("Failed to save filter"),
        },
      );
    } else if (filterId) {
      update.mutate(
        { id: filterId, input: { name: trimmed, visibility, shares } },
        {
          onSuccess: () => {
            toast.success("Filter updated");
            reset();
            close();
          },
          onError: () => toast.error("Failed to update filter"),
        },
      );
    }
  };

  const pending = create.isPending || update.isPending;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          reset();
          close();
        }
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{mode === "edit" ? "Share filter" : "Save filter"}</DialogTitle>
          <DialogDescription>
            Give this filter a name and choose who can use it.
          </DialogDescription>
        </DialogHeader>

        <Input
          autoFocus
          placeholder="e.g. My open bugs"
          value={name}
          maxLength={120}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && visibility !== "shared") {
              e.preventDefault();
              submit();
            }
          }}
        />

        <RadioGroup
          value={visibility}
          onValueChange={(v) => setVisibility(v as SavedFilterVisibility)}
          className="gap-2"
        >
          <Label className="flex cursor-pointer items-start gap-2 font-normal">
            <RadioGroupItem value="private" className="mt-0.5" />
            <span className="flex flex-col gap-0.5">
              <span className="font-medium">Only you</span>
              <span className="text-xs text-muted-foreground">
                A personal filter no one else sees.
              </span>
            </span>
          </Label>

          {canShare && (
            <>
              <Label className="flex cursor-pointer items-start gap-2 font-normal">
                <RadioGroupItem value="shared" className="mt-0.5" />
                <span className="flex flex-col gap-0.5">
                  <span className="font-medium">Selected colleagues &amp; groups</span>
                  <span className="text-xs text-muted-foreground">
                    Only the people and groups you pick can use it.
                  </span>
                </span>
              </Label>
              <Label className="flex cursor-pointer items-start gap-2 font-normal">
                <RadioGroupItem value="workspace" className="mt-0.5" />
                <span className="flex flex-col gap-0.5">
                  <span className="font-medium">Whole team</span>
                  <span className="text-xs text-muted-foreground">
                    Everyone in the workspace can use it.
                  </span>
                </span>
              </Label>
            </>
          )}
        </RadioGroup>

        {!canShare && (
          <p className="text-xs text-muted-foreground">
            Only an admin can let you share filters with others.
          </p>
        )}

        {visibility === "shared" && (
          <div className="rounded-md border">
            <ScrollArea className="h-48">
              <div className="p-1">
                {groups.length > 0 && (
                  <>
                    <p className="px-2 py-1 text-xs font-medium text-muted-foreground">
                      Groups
                    </p>
                    {groups.map((g) => {
                      const key = `group:${g.id}`;
                      return (
                        <label
                          key={key}
                          className="flex cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 hover:bg-accent"
                        >
                          <Checkbox
                            checked={selected.has(key)}
                            onCheckedChange={() =>
                              toggle({ targetType: "group", targetId: g.id })
                            }
                          />
                          <Users className="size-4 text-muted-foreground" />
                          <span className="flex-1 truncate text-sm">{g.name}</span>
                        </label>
                      );
                    })}
                  </>
                )}

                <p className="px-2 py-1 text-xs font-medium text-muted-foreground">
                  People
                </p>
                {members.map((m) => {
                  const key = `member:${m.user_id}`;
                  return (
                    <label
                      key={key}
                      className="flex cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 hover:bg-accent"
                    >
                      <Checkbox
                        checked={selected.has(key)}
                        onCheckedChange={() =>
                          toggle({ targetType: "member", targetId: m.user_id })
                        }
                      />
                      <Avatar className="size-5">
                        {m.avatar_url && <AvatarImage src={m.avatar_url} alt={m.name} />}
                        <AvatarFallback className="text-[10px]">
                          {(m.name || m.email || "?").slice(0, 1).toUpperCase()}
                        </AvatarFallback>
                      </Avatar>
                      <span className="flex-1 truncate text-sm">{m.name || m.email}</span>
                    </label>
                  );
                })}
              </div>
            </ScrollArea>
          </div>
        )}

        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => {
              reset();
              close();
            }}
          >
            Cancel
          </Button>
          <Button onClick={submit} disabled={!name.trim() || pending}>
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
