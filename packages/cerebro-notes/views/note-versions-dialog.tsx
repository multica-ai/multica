"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { History, RotateCcw, Check } from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@multica/ui/components/ui/sheet";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  useNoteVersions,
  useSaveNoteVersion,
  useRestoreNoteVersion,
  type NoteVersion,
} from "../core";

const REASON_LABEL: Record<NoteVersion["reason"], string> = {
  edit: "Edit",
  manual: "Saved checkpoint",
  restore: "Restored",
};

// NoteVersionsDialog (Wave 3 / G2): a note's history, newest first. Each entry
// shows who/when/why; the body is previewed and can be restored. "Save version"
// captures the current state as a labelled checkpoint. Rendered as a full-height
// side sheet (TECH-3637) so a long note body has room without truncation.
export function NoteVersionsDialog({
  noteId,
  open,
  onOpenChange,
}: {
  noteId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: versions = [], isLoading } = useNoteVersions(noteId, open);
  const saveVersion = useSaveNoteVersion(noteId);
  const restore = useRestoreNoteVersion(noteId);
  const [selectedId, setSelectedId] = React.useState<string | null>(null);

  const selected = versions.find((v) => v.id === selectedId) ?? versions[0] ?? null;

  function nameFor(id: string) {
    return members.find((m) => m.user_id === id)?.name ?? "Unknown";
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 sm:max-w-4xl"
      >
        <SheetHeader className="border-b px-5 py-3">
          <SheetTitle className="flex items-center gap-2 text-base">
            <History className="size-4" /> Version history
          </SheetTitle>
          <SheetDescription className="sr-only">
            See who changed this note and restore an earlier version.
          </SheetDescription>
        </SheetHeader>

        <div className="flex min-h-0 flex-1">
          {/* Timeline */}
          <div className="flex w-64 shrink-0 flex-col border-r">
            <div className="flex items-center justify-between px-3 py-2">
              <span className="text-xs text-muted-foreground">
                {versions.length} version{versions.length === 1 ? "" : "s"}
              </span>
              <Button
                size="sm"
                variant="outline"
                onClick={() => saveVersion.mutate(undefined)}
                disabled={saveVersion.isPending}
              >
                <Check className="size-3.5" /> Save version
              </Button>
            </div>
            <ScrollArea className="min-h-0 flex-1">
              {isLoading ? (
                <p className="p-3 text-sm text-muted-foreground">Loading…</p>
              ) : versions.length === 0 ? (
                <p className="p-3 text-sm text-muted-foreground">
                  No versions yet. Edits and saved checkpoints will appear here.
                </p>
              ) : (
                versions.map((v) => (
                  <button
                    key={v.id}
                    onClick={() => setSelectedId(v.id)}
                    className={cn(
                      "block w-full border-b px-3 py-2 text-left hover:bg-muted/50",
                      selected?.id === v.id && "bg-muted",
                    )}
                  >
                    <div className="flex items-center gap-1.5">
                      <Badge variant="outline" className="text-[10px]">
                        v{v.version_no}
                      </Badge>
                      <span className="truncate text-xs font-medium">
                        {v.label || REASON_LABEL[v.reason]}
                      </span>
                    </div>
                    <div className="mt-1 truncate text-[11px] text-muted-foreground">
                      {nameFor(v.author_id)} · {formatWhen(v.created_at)}
                    </div>
                  </button>
                ))
              )}
            </ScrollArea>
          </div>

          {/* Preview */}
          <div className="flex min-w-0 flex-1 flex-col">
            {selected ? (
              <>
                <div className="flex items-center gap-2 border-b px-4 py-2">
                  <span className="text-sm font-medium">
                    {selected.title || "Untitled"}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    v{selected.version_no} · {nameFor(selected.author_id)}
                  </span>
                  <Button
                    size="sm"
                    variant="secondary"
                    className="ml-auto"
                    onClick={() =>
                      restore.mutate(selected.id, {
                        onSuccess: () => onOpenChange(false),
                      })
                    }
                    disabled={restore.isPending}
                  >
                    <RotateCcw className="size-3.5" /> Restore this version
                  </Button>
                </div>
                <ScrollArea className="min-h-0 flex-1">
                  <pre className="whitespace-pre-wrap break-words p-4 text-sm">
                    {selected.body || "(empty)"}
                  </pre>
                </ScrollArea>
              </>
            ) : (
              <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
                Select a version to preview.
              </div>
            )}
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}

function formatWhen(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}
