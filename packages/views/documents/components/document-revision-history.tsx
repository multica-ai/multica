"use client";

import { useState } from "react";
import { Clock, RotateCcw, Bot, User } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";
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
import type { DocumentRevisionSummary } from "@multica/core/types";
import { useT } from "../../i18n";

interface DocumentRevisionHistoryProps {
  revisions: DocumentRevisionSummary[];
  onRestore: (revisionNumber: number) => void;
  onViewRevision: (revisionNumber: number) => void;
}

function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  if (diffMins < 1) return "just now";
  if (diffMins < 60) return `${diffMins}m ago`;
  const diffHrs = Math.floor(diffMins / 60);
  if (diffHrs < 24) return `${diffHrs}h ago`;
  const diffDays = Math.floor(diffHrs / 24);
  if (diffDays < 30) return `${diffDays}d ago`;
  return date.toLocaleDateString();
}

export function DocumentRevisionHistory({
  revisions,
  onRestore,
  onViewRevision,
}: DocumentRevisionHistoryProps) {
  const { t } = useT("documents");
  const [restoreTarget, setRestoreTarget] = useState<number | null>(null);

  if (revisions.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 px-4 py-12 text-center">
        <Clock className="h-8 w-8 text-muted-foreground/30" />
        <p className="text-sm text-muted-foreground">
          {t(($) => $.history.no_revisions)}
        </p>
      </div>
    );
  }

  return (
    <>
      <ScrollArea className="flex-1">
        <div className="space-y-0.5 p-2">
          {revisions.map((rev, idx) => {
            const isLatest = idx === 0;
            const isAgent =
              rev.author_type === "agent_foreground" ||
              rev.author_type === "agent_background";

            return (
              <div
                key={rev.id}
                className="group flex items-start gap-2 rounded-md px-3 py-2 hover:bg-accent/50"
              >
                <div className="mt-0.5 shrink-0">
                  {isAgent ? (
                    <Bot className="h-3.5 w-3.5 text-muted-foreground" />
                  ) : (
                    <User className="h-3.5 w-3.5 text-muted-foreground" />
                  )}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <Badge variant="outline" className="text-[10px]">
                      {t(
                        ($) =>
                          $.history.operation[
                            rev.operation as keyof typeof $.history.operation
                          ],
                      )}
                    </Badge>
                    <span className="text-[10px] text-muted-foreground">
                      {formatRelativeTime(rev.created_at)}
                    </span>
                    {isLatest && (
                      <Badge variant="secondary" className="text-[10px]">
                        {t(($) => $.history.latest)}
                      </Badge>
                    )}
                  </div>
                  {rev.change_summary && (
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      {rev.change_summary}
                    </p>
                  )}
                </div>
                <div className="flex shrink-0 items-center gap-1 opacity-0 group-hover:opacity-100">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 px-1.5 text-[10px]"
                    onClick={() => onViewRevision(rev.revision_number)}
                  >
                    {t(($) => $.history.view_diff)}
                  </Button>
                  {!isLatest && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 px-1.5 text-[10px]"
                      onClick={() => setRestoreTarget(rev.revision_number)}
                    >
                      <RotateCcw className="mr-0.5 h-2.5 w-2.5" />
                      {t(($) => $.history.restore)}
                    </Button>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </ScrollArea>

      <AlertDialog
        open={restoreTarget !== null}
        onOpenChange={(open) => {
          if (!open) setRestoreTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.history.restore_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.history.restore_confirm_description, {
                number: String(restoreTarget ?? 0),
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>
              {t(($) => $.history.restore_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (restoreTarget !== null) {
                  onRestore(restoreTarget);
                  setRestoreTarget(null);
                }
              }}
            >
              {t(($) => $.history.restore_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
