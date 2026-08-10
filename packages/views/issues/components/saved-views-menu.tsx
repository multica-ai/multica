"use client";

import { useState } from "react";
import { Bookmark, Check, Pencil, Plus, Trash2, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
} from "@multica/ui/components/ui/dropdown-menu";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  useSavedViewsStore,
  selectSavedViews,
  applySavedViewState,
} from "@multica/core/issues/stores/saved-views-store";
import { useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import { useT } from "../../i18n";
import { cn } from "@multica/ui/lib/utils";

/**
 * "Saved views" dropdown for the issues header. Lets a user snapshot the
 * current filter / sort / view-mode / column configuration under a name and
 * switch between presets without re-picking filters.
 *
 * Phase 1 is per-user and local: presets are persisted in browser storage and
 * namespaced by workspace id. Sharing presets with teammates is a follow-up.
 */
export function SavedViewsMenu({
  triggerClassName,
}: {
  triggerClassName?: string;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const savedViews = useSavedViewsStore(selectSavedViews(wsId));
  const saveView = useSavedViewsStore((s) => s.saveView);
  const renameView = useSavedViewsStore((s) => s.renameView);
  const deleteView = useSavedViewsStore((s) => s.deleteView);
  const viewStoreApi = useViewStoreApi();

  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveName, setSaveName] = useState("");
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameName, setRenameName] = useState("");

  const applyView = (id: string) => {
    const view = savedViews.find((candidate) => candidate.id === id);
    if (!view) return;
    viewStoreApi.setState(applySavedViewState(view.state));
    setOpen(false);
  };

  const confirmSave = () => {
    const name = saveName.trim();
    if (!name) return;
    saveView(wsId, viewStoreApi.getState(), name);
    setSaveName("");
    setSaving(false);
  };

  const startRename = (id: string, name: string) => {
    setRenamingId(id);
    setRenameName(name);
  };

  const confirmRename = () => {
    const name = renameName.trim();
    if (renamingId && name) renameView(wsId, renamingId, name);
    setRenamingId(null);
    setRenameName("");
  };

  const cancelRename = () => {
    setRenamingId(null);
    setRenameName("");
  };

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <Tooltip>
        <DropdownMenuTrigger
          render={
            <TooltipTrigger
              render={
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  aria-label={t(($) => $.saved_views.menu_tooltip)}
                  className={cn("shrink-0", triggerClassName)}
                >
                  <Bookmark className="size-3.5" />
                  {savedViews.length > 0 && (
                    <span className="text-caption text-muted-foreground">
                      {savedViews.length}
                    </span>
                  )}
                </Button>
              }
            />
          }
        />
        <TooltipContent side="bottom">
          {t(($) => $.saved_views.menu_tooltip)}
        </TooltipContent>
      </Tooltip>

      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t(($) => $.saved_views.menu_label)}</DropdownMenuLabel>
        </DropdownMenuGroup>

        {savedViews.length === 0 ? (
          <p className="px-1.5 py-1.5 text-caption text-muted-foreground">
            {t(($) => $.saved_views.empty)}
          </p>
        ) : (
          <div className="max-h-64 overflow-y-auto">
            {savedViews.map((view) => (
              <DropdownMenuSub key={view.id}>
                <DropdownMenuSubTrigger className="gap-2">
                  <Bookmark className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate">{view.name}</span>
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent alignOffset={-4} sideOffset={-2}>
                  {renamingId === view.id ? (
                    <div className="flex items-center gap-1 p-1">
                      <Input
                        autoFocus
                        value={renameName}
                        onChange={(event) => setRenameName(event.target.value)}
                        onKeyDown={(event) => {
                          if (event.key === "Enter") confirmRename();
                          if (event.key === "Escape") cancelRename();
                        }}
                        placeholder={t(($) => $.saved_views.name_placeholder)}
                        className="h-7 text-body"
                      />
                      <Button
                        type="button"
                        size="icon-sm"
                        aria-label={t(($) => $.saved_views.save)}
                        onClick={confirmRename}
                        disabled={!renameName.trim()}
                      >
                        <Check />
                      </Button>
                      <Button
                        type="button"
                        size="icon-sm"
                        variant="ghost"
                        aria-label={t(($) => $.saved_views.cancel)}
                        onClick={cancelRename}
                      >
                        <X />
                      </Button>
                    </div>
                  ) : (
                    <>
                      <DropdownMenuItem onClick={() => applyView(view.id)}>
                        <Bookmark />
                        {t(($) => $.saved_views.apply)}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        closeOnClick={false}
                        onClick={() => startRename(view.id, view.name)}
                      >
                        <Pencil />
                        {t(($) => $.saved_views.rename)}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        variant="destructive"
                        onClick={() => deleteView(wsId, view.id)}
                      >
                        <Trash2 />
                        {t(($) => $.saved_views.delete)}
                      </DropdownMenuItem>
                    </>
                  )}
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            ))}
          </div>
        )}

        <DropdownMenuSeparator />

        {saving ? (
          <div className="flex items-center gap-1 p-1">
            <Input
              autoFocus
              value={saveName}
              onChange={(event) => setSaveName(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") confirmSave();
                if (event.key === "Escape") {
                  setSaveName("");
                  setSaving(false);
                }
              }}
              placeholder={t(($) => $.saved_views.name_placeholder)}
              className="h-7 text-body"
            />
            <Button
              type="button"
              size="icon-sm"
              aria-label={t(($) => $.saved_views.save)}
              onClick={confirmSave}
              disabled={!saveName.trim()}
            >
              <Check />
            </Button>
            <Button
              type="button"
              size="icon-sm"
              variant="ghost"
              aria-label={t(($) => $.saved_views.cancel)}
              onClick={() => {
                setSaveName("");
                setSaving(false);
              }}
            >
              <X />
            </Button>
          </div>
        ) : (
          <DropdownMenuItem closeOnClick={false} onClick={() => setSaving(true)}>
            <Plus />
            {t(($) => $.saved_views.save_current)}
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
