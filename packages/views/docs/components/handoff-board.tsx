"use client";

import { lazy, Suspense, useSyncExternalStore } from "react";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useT } from "../../i18n";
import { readBoard } from "./handoff-board-model";

const Canvas = lazy(async () => {
  const assets = window.location.protocol === "file:" ? "./assets/excalidraw/" : "/assets/excalidraw/";
  Object.assign(window, { EXCALIDRAW_ASSET_PATH: new URL(assets, window.location.href).href });
  return import("./handoff-board-canvas");
});

// The drawing library measures the DOM during import. Defer its lazy boundary
// until hydration, including when a prefetched Doc renders on the server.
const subscribe = () => () => {};
const mounted = () => true;
const server = () => false;

export interface HandoffBoardProps {
  markdown: string;
  workspaceId: string;
  workspaceSlug: string;
  onSave: (markdown: string) => Promise<void>;
}

export function HandoffBoard({ markdown, ...props }: HandoffBoardProps) {
  const { t } = useT("docs");
  const hydrated = useSyncExternalStore(subscribe, mounted, server);
  const scene = readBoard(markdown);
  if (!scene) return <p role="alert">{t(($) => $.board.invalid)}</p>;
  return (
    <Suspense fallback={<Skeleton className="h-[60vh] w-full" />}>
      {hydrated ? <Canvas initialData={scene} {...props} /> : <Skeleton className="h-[60vh] w-full" />}
    </Suspense>
  );
}
