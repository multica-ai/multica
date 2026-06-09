"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink, Pencil, Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
  SheetFooter,
} from "@multica/ui/components/ui/sheet";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { artifactDetailOptions } from "@multica/cerebro-artifacts/core";
import {
  useUpdateArtifact,
  useDeleteArtifact,
} from "@multica/cerebro-artifacts/core";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { ArtifactContent } from "./artifact-content";
import { KindIcon, KIND_LABELS } from "./kind-icon";
import { MoveScopeMenu } from "./move-scope-menu";

function formatDateTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function ArtifactSheet({
  artifactId,
  open,
  onOpenChange,
}: {
  artifactId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const wsId = useWorkspaceId();
  const { data: artifact, isLoading } = useQuery(
    artifactDetailOptions(wsId, artifactId),
  );
  const userId = useAuthStore((s) => s.user?.id);
  const { getActorName } = useActorName();
  const wsPaths = useWorkspacePaths();
  const nav = useNavigation();

  const update = useUpdateArtifact();
  const remove = useDeleteArtifact();

  const [editing, setEditing] = React.useState(false);
  const [draftTitle, setDraftTitle] = React.useState("");
  const [draftBody, setDraftBody] = React.useState("");
  const [confirmDelete, setConfirmDelete] = React.useState(false);

  const DEFAULT_WIDTH = 900;
  const [sheetWidth, setSheetWidth] = React.useState(DEFAULT_WIDTH);
  const isResizing = React.useRef(false);
  const resizeStartX = React.useRef(0);
  const resizeStartWidth = React.useRef(0);

  const handleResizeMouseDown = React.useCallback(
    (e: React.MouseEvent) => {
      isResizing.current = true;
      resizeStartX.current = e.clientX;
      resizeStartWidth.current = sheetWidth;
      e.preventDefault();
    },
    [sheetWidth],
  );

  React.useEffect(() => {
    const onMouseMove = (e: MouseEvent) => {
      if (!isResizing.current) return;
      const delta = resizeStartX.current - e.clientX;
      const next = Math.max(400, Math.min(window.innerWidth - 60, resizeStartWidth.current + delta));
      setSheetWidth(next);
    };
    const onMouseUp = () => { isResizing.current = false; };
    document.addEventListener("mousemove", onMouseMove);
    document.addEventListener("mouseup", onMouseUp);
    return () => {
      document.removeEventListener("mousemove", onMouseMove);
      document.removeEventListener("mouseup", onMouseUp);
    };
  }, []);

  const openInDocuments = React.useCallback(() => {
    if (!artifact) return;
    const path = wsPaths.documentDetail(artifact.id);
    if (nav.openInNewTab) {
      nav.openInNewTab(path);
    } else if (nav.getShareableUrl) {
      window.open(nav.getShareableUrl(path), "_blank", "noopener,noreferrer");
    } else {
      window.open(path, "_blank", "noopener,noreferrer");
    }
  }, [artifact, wsPaths, nav]);

  React.useEffect(() => {
    if (artifact && !editing) {
      setDraftTitle(artifact.title);
      setDraftBody(artifact.body);
    }
  }, [artifact, editing]);

  React.useEffect(() => {
    if (!open) {
      setEditing(false);
      setConfirmDelete(false);
    }
  }, [open]);

  const canEdit =
    artifact &&
    artifact.author_type === "member" &&
    artifact.author_id === userId;

  const handleSave = async () => {
    if (!artifact) return;
    await update.mutateAsync({
      id: artifact.id,
      data: { title: draftTitle, body: draftBody },
    });
    setEditing(false);
  };

  const handleDelete = async () => {
    if (!artifact) return;
    await remove.mutateAsync(artifact);
    setConfirmDelete(false);
    onOpenChange(false);
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        className="w-auto sm:max-w-none"
        style={{ width: sheetWidth, maxWidth: "none" }}
      >
        {/* Drag-to-resize handle on the left edge */}
        <div
          className="absolute left-0 top-0 h-full w-1.5 cursor-col-resize hover:bg-primary/20 transition-colors z-10"
          onMouseDown={handleResizeMouseDown}
        />
        {isLoading || !artifact ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            Loading…
          </div>
        ) : (
          <>
            <SheetHeader className="gap-2">
              <div className="flex items-center gap-2">
                <KindIcon kind={artifact.kind} className="size-4 text-muted-foreground" />
                <Badge variant="secondary" className="text-xs font-normal">
                  {KIND_LABELS[artifact.kind]}
                </Badge>
                {artifact.author_type === "agent" && (
                  <Badge variant="outline" className="text-xs">
                    agent
                  </Badge>
                )}
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="ml-auto"
                  onClick={openInDocuments}
                  title="Open in Documents"
                >
                  <ExternalLink className="size-4" />
                  <span className="sr-only">Open in Documents</span>
                </Button>
              </div>
              {editing ? (
                <Input
                  value={draftTitle}
                  onChange={(e) => setDraftTitle(e.target.value)}
                  placeholder="Title"
                  className="text-base"
                />
              ) : (
                <SheetTitle className="text-lg leading-tight">
                  {artifact.title}
                </SheetTitle>
              )}
              <SheetDescription className="text-xs">
                {getActorName(artifact.author_type, artifact.author_id)} ·{" "}
                {formatDateTime(artifact.updated_at)}
              </SheetDescription>
            </SheetHeader>

            <div className="flex-1 overflow-y-auto px-4 pb-4">
              {editing ? (
                <Textarea
                  value={draftBody}
                  onChange={(e) => setDraftBody(e.target.value)}
                  placeholder="Markdown body"
                  className="min-h-[400px] font-mono text-sm"
                />
              ) : (
                <ArtifactContent
                  artifact={artifact}
                  frameClassName="h-[calc(100vh-14rem)] min-h-[520px]"
                />
              )}
            </div>

            <SheetFooter className="flex-row justify-between border-t">
              <div className="flex items-center gap-2">
                {canEdit && !editing && (
                  <>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setEditing(true)}
                    >
                      <Pencil className="mr-1 size-4" /> Edit
                    </Button>
                    <MoveScopeMenu artifact={artifact} />
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setConfirmDelete(true)}
                      className="text-destructive hover:text-destructive"
                    >
                      <Trash2 className="mr-1 size-4" /> Delete
                    </Button>
                  </>
                )}
              </div>
              {editing && (
                <div className="flex items-center gap-2">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setEditing(false);
                      setDraftTitle(artifact.title);
                      setDraftBody(artifact.body);
                    }}
                    disabled={update.isPending}
                  >
                    Cancel
                  </Button>
                  <Button
                    size="sm"
                    onClick={handleSave}
                    disabled={update.isPending || !draftTitle.trim()}
                  >
                    {update.isPending ? "Saving…" : "Save"}
                  </Button>
                </div>
              )}
            </SheetFooter>

            <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Delete artifact?</AlertDialogTitle>
                  <AlertDialogDescription>
                    This permanently removes &ldquo;{artifact.title}&rdquo;.
                    Cannot be undone.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>Cancel</AlertDialogCancel>
                  <AlertDialogAction
                    onClick={handleDelete}
                    disabled={remove.isPending}
                  >
                    {remove.isPending ? "Deleting…" : "Delete"}
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
