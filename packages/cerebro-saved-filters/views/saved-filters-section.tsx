"use client";

import { useState } from "react";
import { Bookmark, Check, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { useWorkspaceId } from "@multica/core/hooks";
import { useViewStore, useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
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

import {
  useCreateSavedFilter,
  useDeleteSavedFilter,
  useSavedFilters,
} from "../core/queries";
import {
  extractFilterSnapshot,
  isSnapshotEmpty,
  snapshotToState,
  snapshotsEqual,
} from "../core/filter-snapshot";
import { useSaveFilterDialogStore } from "./save-dialog-store";

const DEFAULT_SURFACE = "issues";

/**
 * SavedFiltersMenuSection renders at the top of the issue Filter dropdown: the
 * member's personal saved filters (one-click apply + delete) and a "Save current
 * filter…" action. Hidden entirely when the cerebro_saved_filters flag is off.
 * Render it INSIDE the Filter DropdownMenuContent; pair it with
 * <SaveFilterDialogHost /> mounted outside the dropdown (see views/index).
 */
export function SavedFiltersMenuSection() {
  const enabled = useFeatureFlag("cerebro_saved_filters");
  const wsId = useWorkspaceId();
  const storeApi = useViewStoreApi();
  // Subscribe so the active-filter highlight re-evaluates as the user edits.
  const liveStatus = useViewStore((s) => s.statusFilters);
  const { data: filters = [] } = useSavedFilters(wsId, undefined);
  const requestSave = useSaveFilterDialogStore((s) => s.requestSave);
  const del = useDeleteSavedFilter(wsId);

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

  return (
    <>
      <DropdownMenuLabel className="flex items-center gap-1.5 text-muted-foreground">
        <Bookmark className="size-3.5" />
        Saved filters
      </DropdownMenuLabel>

      {filters.length === 0 ? (
        <DropdownMenuItem disabled className="text-xs text-muted-foreground">
          No saved filters yet
        </DropdownMenuItem>
      ) : (
        filters.map((f) => {
          const active = snapshotsEqual(current, f.filterState);
          return (
            <DropdownMenuItem
              key={f.id}
              className="group flex items-center gap-2"
              onClick={() => storeApi.setState(snapshotToState(f.filterState))}
            >
              <Check
                className={active ? "size-3.5 text-primary" : "size-3.5 opacity-0"}
              />
              <span className="flex-1 truncate">{f.name}</span>
              <span
                role="button"
                tabIndex={-1}
                aria-label={`Delete ${f.name}`}
                className="ml-1 hidden rounded-sm p-0.5 text-muted-foreground hover:bg-destructive/10 hover:text-destructive group-hover:inline-flex"
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
            </DropdownMenuItem>
          );
        })
      )}

      <DropdownMenuItem onClick={onSave}>
        <Plus className="size-3.5" />
        <span>Save current filter…</span>
      </DropdownMenuItem>

      <DropdownMenuSeparator />
    </>
  );
}

/**
 * SaveFilterDialogHost owns the naming dialog. It lives OUTSIDE the Filter
 * dropdown so it survives the dropdown closing when "Save current filter…" is
 * selected. Mount it once, next to the Filter dropdown.
 */
export function SaveFilterDialogHost({ surface }: { surface?: string }) {
  const enabled = useFeatureFlag("cerebro_saved_filters");
  const wsId = useWorkspaceId();
  const open = useSaveFilterDialogStore((s) => s.open);
  const snapshot = useSaveFilterDialogStore((s) => s.snapshot);
  const close = useSaveFilterDialogStore((s) => s.close);
  const create = useCreateSavedFilter(wsId);
  const [name, setName] = useState("");

  if (!enabled) return null;

  const submit = () => {
    const trimmed = name.trim();
    if (!trimmed || !snapshot) return;
    create.mutate(
      { name: trimmed, surface: surface ?? DEFAULT_SURFACE, filterState: snapshot },
      {
        onSuccess: () => {
          toast.success("Filter saved");
          setName("");
          close();
        },
        onError: () => toast.error("Failed to save filter"),
      },
    );
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          setName("");
          close();
        }
      }}
    >
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Save filter</DialogTitle>
          <DialogDescription>
            Give this filter a name to find it again with one click. Only you can
            see it.
          </DialogDescription>
        </DialogHeader>
        <Input
          autoFocus
          placeholder="e.g. My open bugs"
          value={name}
          maxLength={120}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              submit();
            }
          }}
        />
        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => {
              setName("");
              close();
            }}
          >
            Cancel
          </Button>
          <Button onClick={submit} disabled={!name.trim() || create.isPending}>
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
