"use client";

import * as React from "react";
import { useMutation } from "@tanstack/react-query";
import { api, ApiError } from "@multica/core/api";
import { Loader2, GitMerge, User, Server } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";

// NoteConflict holds the data needed to resolve a concurrent-edit conflict.
export type NoteConflict = {
  noteId: string;
  // yourBody is the body the current user tried to save.
  yourBody: string;
  // serverBody is the body that was already on the server (someone else's edit).
  serverBody: string;
};

type Props = {
  conflict: NoteConflict | null;
  onResolve: (resolvedBody: string) => void;
  onCancel: () => void;
};

// NoteConflictDialog shows two concurrent note versions plus an AI-proposed
// merge. The user can accept the AI merge, pick one version, or cancel.
// Opened automatically when the server returns 409 on a note save (FIR-1317 Plan A).
export function NoteConflictDialog({ conflict, onResolve, onCancel }: Props) {
  const open = conflict !== null;
  const [mergedText, setMergedText] = React.useState<string | null>(null);
  const [mergeError, setMergeError] = React.useState(false);

  const mergeMutation = useMutation({
    mutationFn: async ({ noteId, yourBody, serverBody }: NoteConflict) => {
      try {
        const res = await api.mergeNote<{ merged: string }>(noteId, {
          your_body: yourBody,
          server_body: serverBody,
        });
        return res.merged ?? "";
      } catch (err) {
        if (err instanceof ApiError && err.status === 503) {
          return null;
        }
        throw err;
      }
    },
    onSuccess: (merged) => {
      if (merged !== null) {
        setMergedText(merged);
      } else {
        setMergeError(true);
      }
    },
    onError: () => setMergeError(true),
  });

  React.useEffect(() => {
    if (!conflict) {
      setMergedText(null);
      setMergeError(false);
      return;
    }
    mergeMutation.mutate(conflict);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conflict?.noteId, conflict?.yourBody, conflict?.serverBody]);

  if (!conflict) return null;

  const loading = mergeMutation.isPending;
  const hasMerge = mergedText !== null;

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) onCancel(); }}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <GitMerge className="size-5 text-amber-500" />
            Conflict: two people edited this note at the same time
          </DialogTitle>
          <DialogDescription>
            Someone else saved changes while you were editing. Review the versions below
            and choose how to proceed.
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue={hasMerge ? "merge" : "yours"} className="mt-1">
          <TabsList className="w-full">
            {hasMerge && (
              <TabsTrigger value="merge" className="flex-1 gap-1.5">
                <GitMerge className="size-3.5" />
                AI merge
                <span className="rounded bg-amber-100 px-1 text-[10px] font-medium text-amber-700">
                  recommended
                </span>
              </TabsTrigger>
            )}
            <TabsTrigger value="yours" className="flex-1 gap-1.5">
              <User className="size-3.5" /> Your version
            </TabsTrigger>
            <TabsTrigger value="theirs" className="flex-1 gap-1.5">
              <Server className="size-3.5" /> Their version
            </TabsTrigger>
          </TabsList>

          {hasMerge && (
            <TabsContent value="merge" className="mt-3">
              <ScrollArea className="h-64 rounded-md border bg-muted/30 p-3">
                <pre className="whitespace-pre-wrap font-sans text-sm leading-relaxed">
                  {mergedText}
                </pre>
              </ScrollArea>
              <p className="mt-2 text-xs text-muted-foreground">
                AI-suggested merge. Review before accepting — it may miss nuance.
              </p>
            </TabsContent>
          )}

          <TabsContent value="yours" className="mt-3">
            <ScrollArea className="h-64 rounded-md border bg-muted/30 p-3">
              <pre className="whitespace-pre-wrap font-sans text-sm leading-relaxed">
                {conflict.yourBody || <span className="text-muted-foreground italic">Empty</span>}
              </pre>
            </ScrollArea>
          </TabsContent>

          <TabsContent value="theirs" className="mt-3">
            <ScrollArea className="h-64 rounded-md border bg-muted/30 p-3">
              <pre className="whitespace-pre-wrap font-sans text-sm leading-relaxed">
                {conflict.serverBody || <span className="text-muted-foreground italic">Empty</span>}
              </pre>
            </ScrollArea>
          </TabsContent>
        </Tabs>

        {loading && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" />
            Asking AI to suggest a merge…
          </div>
        )}
        {mergeError && (
          <p className="text-sm text-amber-600">
            AI merge unavailable — pick a version manually below.
          </p>
        )}

        <DialogFooter className="flex-wrap gap-2 sm:justify-start">
          {hasMerge && (
            <Button onClick={() => onResolve(mergedText ?? conflict.yourBody)}>
              Use AI merge
            </Button>
          )}
          <Button
            variant={hasMerge ? "outline" : "default"}
            onClick={() => onResolve(conflict.yourBody)}
          >
            Keep mine
          </Button>
          <Button variant="outline" onClick={() => onResolve(conflict.serverBody)}>
            Keep theirs
          </Button>
          <Button variant="ghost" onClick={onCancel} className="ml-auto">
            Cancel
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
