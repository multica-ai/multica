"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ArrowUp, Folder, Loader2 } from "@multica/ui/icons";
import { api } from "@multica/core/api";
import { runtimeDirectoryRequestOptions } from "@multica/core/runtimes/queries";
import type { ChatWorkingDirectory } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";

interface ChatDirectoryDialogProps {
  wsId: string;
  runtimeId: string;
  daemonId: string;
  initialPath?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSelect: (directory: ChatWorkingDirectory) => void;
}

export function ChatDirectoryDialog(props: ChatDirectoryDialogProps) {
  return props.open ? (
    <DirectoryBrowser
      key={`${props.wsId}:${props.runtimeId}:${props.daemonId}`}
      {...props}
    />
  ) : null;
}

function DirectoryBrowser({
  wsId,
  runtimeId,
  daemonId,
  initialPath = "",
  open,
  onOpenChange,
  onSelect,
}: ChatDirectoryDialogProps) {
  const { t } = useT("chat");
  const [requestedPath, setRequestedPath] = useState(initialPath);
  const [pathDraft, setPathDraft] = useState<string | null>(null);
  const { mutate, ...createRequest } = useMutation({
    mutationKey: ["runtimes", wsId, runtimeId, "directories", "create"],
    mutationFn: (path: string) => api.createRuntimeDirectoryRequest(runtimeId, path),
  });
  useEffect(() => { mutate(initialPath); }, [mutate, initialPath]);
  const polled = useQuery({
    ...runtimeDirectoryRequestOptions(wsId, runtimeId, createRequest.data?.id ?? ""),
    enabled: Boolean(createRequest.data?.id) &&
      (createRequest.data?.status === "pending" || createRequest.data?.status === "running"),
  });
  const request = polled.data ?? createRequest.data;
  const failed = createRequest.isError || polled.isError ||
    request?.status === "failed" || request?.status === "timeout" ||
    (request?.status === "completed" && (!request.result || request.runtime_id !== runtimeId));
  const loading = !failed && (
    createRequest.isPending || request?.status === "pending" || request?.status === "running"
  );
  const result = !loading && !failed && request?.runtime_id === runtimeId &&
    request.status === "completed" ? request.result : null;
  const path = pathDraft ?? result?.path ?? requestedPath;
  const browse = (nextPath: string) => {
    setRequestedPath(nextPath);
    setPathDraft(null);
    mutate(nextPath);
  };
  const canSelect = result?.selectable === true && path === result.path;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[80vh] flex-col sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.directory.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.directory.new_conversation)}</DialogDescription>
        </DialogHeader>
        <form
          className="flex items-end gap-2"
          onSubmit={(event) => { event.preventDefault(); browse(path); }}
        >
          <label className="min-w-0 flex-1 text-caption font-medium">
            {t(($) => $.directory.path)}
            <Input
              value={path}
              onChange={(event) => setPathDraft(event.target.value)}
              disabled={loading}
              className="mt-1"
            />
          </label>
          <Button type="submit" variant="outline" disabled={loading}>
            {t(($) => $.directory.browse)}
          </Button>
        </form>
        <div className="min-h-40 min-w-0 flex-1 overflow-y-auto rounded-md border border-surface-border">
          {loading ? (
            <div role="status" className="flex items-center justify-center gap-2 p-8 text-caption text-muted-foreground">
              <Loader2 className="size-4 animate-spin" />
              {t(($) => $.directory.loading)}
            </div>
          ) : failed ? (
            <p role="alert" className="p-4 text-caption text-destructive">
              {request?.error || t(($) => $.directory.failed)}
            </p>
          ) : result ? (
            <>
              {result.parent_path && (
                <Button
                  variant="ghost"
                  className="w-full justify-start rounded-none"
                  onClick={() => browse(result.parent_path ?? "")}
                >
                  <ArrowUp className="size-4" />
                  {t(($) => $.directory.parent)}
                </Button>
              )}
              {result.entries.map((entry) => (
                <Button
                  key={entry.path}
                  variant="ghost"
                  className="w-full justify-start rounded-none"
                  onClick={() => browse(entry.path)}
                >
                  <Folder className="size-4 shrink-0" />
                  <span className="truncate">{entry.name}</span>
                </Button>
              ))}
              {result.entries.length === 0 && (
                <p className="p-4 text-caption text-muted-foreground">
                  {t(($) => $.directory.empty)}
                </p>
              )}
              {!result.selectable && (
                <p role="alert" className="p-4 text-caption text-warning">
                  {t(($) => $.directory.not_selectable)}
                </p>
              )}
            </>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t(($) => $.directory.cancel)}
          </Button>
          <Button
            disabled={!canSelect}
            onClick={() => {
              if (!canSelect || !result) return;
              onSelect({ daemon_id: daemonId, path: result.path });
              onOpenChange(false);
            }}
          >
            {t(($) => $.directory.select)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
