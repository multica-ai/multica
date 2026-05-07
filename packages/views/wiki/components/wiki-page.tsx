"use client";

import { useCallback, useEffect, useRef } from "react";
import { BookOpenText } from "lucide-react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";
import {
  memberListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { ContentEditor, type ContentEditorRef, ReadonlyContent } from "../../editor";
import { PageHeader } from "../../layout/page-header";

export function WikiPage() {
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const editorRef = useRef<ContentEditorRef>(null);
  const lastSavedContentRef = useRef<string | null>(null);
  const { data: members = [], isLoading: membersLoading } = useQuery(memberListOptions(wsId));

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canEdit = currentMember?.role === "owner" || currentMember?.role === "admin";
  const content = workspace?.wiki_content ?? "";

  useEffect(() => {
    lastSavedContentRef.current = content;
  }, [content, workspace?.id]);

  const handleUpdate = useCallback(async (wikiContent: string) => {
    if (!workspace || !canEdit) return;
    if (wikiContent === lastSavedContentRef.current) return;
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        wiki_content: wikiContent,
      });
      lastSavedContentRef.current = updated.wiki_content ?? "";
      queryClient.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to save wiki");
    }
  }, [canEdit, queryClient, workspace]);

  const handleBlur = useCallback(() => {
    const latestContent = editorRef.current?.getMarkdown();
    if (latestContent == null) return;
    void handleUpdate(latestContent);
  }, [handleUpdate]);

  if (!workspace || membersLoading) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <PageHeader>
          <Skeleton className="h-5 w-5 rounded" />
          <Skeleton className="ml-2 h-4 w-20" />
        </PageHeader>
        <div className="mx-auto w-full max-w-4xl px-8 py-8">
          <Skeleton className="h-8 w-48" />
          <Skeleton className="mt-6 h-40 w-full" />
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader>
        <div className="flex items-center gap-2">
          <BookOpenText className="size-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">Wiki</h1>
        </div>
        <span className="ml-auto text-xs text-muted-foreground">
          {canEdit ? "Owner/Admin can edit" : "Read-only"}
        </span>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-4xl px-8 py-8">
          <h2 className="text-2xl font-bold leading-snug tracking-tight">Wiki</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            {workspace.name}
          </p>

          <div className={cn("mt-6", canEdit && "rounded-lg")}>
            {canEdit ? (
              <ContentEditor
                ref={editorRef}
                key={workspace.id}
                defaultValue={content}
                placeholder="Add wiki content..."
                onUpdate={handleUpdate}
                onBlur={handleBlur}
                debounceMs={1500}
              />
            ) : content.trim() ? (
              <ReadonlyContent content={content} />
            ) : (
              <p className="text-sm text-muted-foreground">No wiki content yet.</p>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
