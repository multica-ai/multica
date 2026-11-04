import { useEffect, useState, useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowUp, Folder, Loader2 } from "lucide-react";
import { z } from "zod";
import { parseWithFallback } from "@multica/core/api/schema";
import type { LocalV2ChooseRequest, LocalV2Scope } from "@multica/core/local-conversation/v2";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { useLocalDevice } from "../platform/local-device";
import { conversationService } from "../platform/conversation-services";
import { localV2Error } from "@multica/conversation-client/services";

const listingSchema = z.object({
  path: z.string(), parent: z.string().default(""),
  entries: z.array(z.object({ name: z.string(), path: z.string() })).default([]),
});

interface PickRequest { deviceId: string; scope: LocalV2Scope; request: LocalV2ChooseRequest; resolve(path: string | null): void }

let pending: PickRequest | null = null;
const listeners = new Set<() => void>();
const publish = (next: PickRequest | null) => { pending = next; for (const listener of listeners) listener(); };

/**
 * Asks the user for a folder on another device. That device's OS picker is
 * out of reach, so the picker on the open Device link page lists its
 * folders instead. Resolves null when cancelled.
 */
export function pickRemoteFolder(deviceId: string, scope: LocalV2Scope, request: LocalV2ChooseRequest): Promise<string | null> {
  pending?.resolve(null);
  return new Promise((resolve) => publish({ deviceId, scope, request, resolve }));
}

/** The folder picker of a Device link page. */
export function RemoteFolderPicker({ chinese }: { chinese: boolean }) {
  const device = useLocalDevice();
  const request = useSyncExternalStore((listener) => { listeners.add(listener); return () => { listeners.delete(listener); }; }, () => pending);
  const open = request?.deviceId === device.id;
  const [path, setPath] = useState("");
  useEffect(() => { if (open) setPath("defaultPath" in request.request && request.request.defaultPath ? request.request.defaultPath : ""); }, [open, request]);
  const listing = useQuery({
    queryKey: ["device-link", device.id, "directories", path],
    queryFn: async () => parseWithFallback<z.infer<typeof listingSchema>>(await conversationService(device.transport, request!.scope).request("GET", `/directories?${new URLSearchParams({ path })}`), listingSchema, { path, parent: "", entries: [] }, { endpoint: "local-v2.directories" }),
    enabled: open,
    retry: false,
  });
  const finish = (value: string | null) => { request?.resolve(value); publish(null); };
  const current = listing.data?.path ?? path;
  return <Dialog open={open} onOpenChange={(next) => { if (!next) finish(null); }}>
    <DialogContent className="flex max-h-[80vh] flex-col">
      <DialogHeader><DialogTitle>{chinese ? `选择 ${device.name} 上的文件夹` : `Choose a folder on ${device.name}`}</DialogTitle></DialogHeader>
      <div className="flex min-w-0 items-center gap-2">
        <IconButton label={chinese ? "上一级" : "Up"} disabled={!listing.data?.parent} onClick={() => setPath(listing.data!.parent)}><ArrowUp /></IconButton>
        <span className="min-w-0 truncate text-caption text-muted-foreground" title={current}>{current}</span>
      </div>
      <div className="min-h-48 flex-1 overflow-y-auto rounded-lg border" role="listbox" aria-label={current}>
        {listing.isPending ? <div className="flex h-48 items-center justify-center"><Loader2 className="size-4 animate-spin text-muted-foreground" /></div>
          : listing.isError ? <p className="p-3 text-caption text-muted-foreground">{localV2Error(listing.error).message}</p>
          : listing.data.entries.map((entry) => <button key={entry.path} type="button" role="option" aria-selected={false} className="flex w-full min-w-0 items-center gap-2 px-3 py-2 text-left text-body hover:bg-accent/60 focus-visible:bg-accent focus-visible:outline-none" onClick={() => setPath(entry.path)}>
            <Folder className="size-4 shrink-0 text-muted-foreground" /><span className="truncate">{entry.name}</span>
          </button>)}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={() => finish(null)}>{chinese ? "取消" : "Cancel"}</Button>
        <Button disabled={!listing.data} onClick={() => finish(current)}>{chinese ? "选择" : "Choose"}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>;
}
