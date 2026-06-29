"use client";

import { useState } from "react";
import { Archive, Pin, PinOff } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import type { WorkspaceDocument } from "@multica/core/types";
import { useT } from "../../i18n";

interface DocumentHeaderProps {
  document: WorkspaceDocument;
  onTogglePin: () => void;
  onArchive: () => void;
}

export function DocumentHeader({
  document,
  onTogglePin,
  onArchive,
}: DocumentHeaderProps) {
  const { t } = useT("documents");
  const [archiveOpen, setArchiveOpen] = useState(false);

  return (
    <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
      <div className="flex min-w-0 items-center gap-2">
        <span className="truncate font-mono text-sm text-muted-foreground">
          {document.path}
        </span>
        {document.pinned && (
          <Badge variant="secondary" className="shrink-0 text-[10px]">
            <Pin className="mr-0.5 h-2.5 w-2.5" />
            {t(($) => $.header.pin)}
          </Badge>
        )}
        {document.tags.map((tag) => (
          <Badge key={tag} variant="outline" className="shrink-0 text-[10px]">
            {tag}
          </Badge>
        ))}
      </div>

      <div className="flex items-center gap-1">
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-7 p-0"
                onClick={onTogglePin}
              />
            }
          >
            {document.pinned ? (
              <PinOff className="h-3.5 w-3.5" />
            ) : (
              <Pin className="h-3.5 w-3.5" />
            )}
          </TooltipTrigger>
          <TooltipContent>
            {document.pinned ? t(($) => $.header.unpin) : t(($) => $.header.pin)}
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-7 p-0 text-muted-foreground hover:text-destructive"
                onClick={() => setArchiveOpen(true)}
              />
            }
          >
            <Archive className="h-3.5 w-3.5" />
          </TooltipTrigger>
          <TooltipContent>{t(($) => $.header.archive)}</TooltipContent>
        </Tooltip>
      </div>

      <AlertDialog open={archiveOpen} onOpenChange={setArchiveOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.header.archive_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.header.archive_confirm_description, {
                path: document.path,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.header.archive_cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                onArchive();
                setArchiveOpen(false);
              }}
            >
              {t(($) => $.header.archive_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
