"use client";

import { createContext, use, useMemo, type ReactNode } from "react";
import type { DeliverableFile } from "@multica/core/attachments/deliverables";

/**
 * Which version of its file each comment upload is (MUL-7649), so a comment's
 * file card can say `v2` without every card re-deriving the issue's
 * deliverables. Only files uploaded more than once are in the map; surfaces
 * without a provider (chat) show no versions.
 */
const AttachmentVersionsContext = createContext<ReadonlyMap<string, number> | null>(null);

export function AttachmentVersionsProvider({
  files,
  children,
}: {
  files: ReadonlyArray<DeliverableFile>;
  children: ReactNode;
}) {
  const versions = useMemo(() => {
    const map = new Map<string, number>();
    for (const file of files) {
      if (file.versions.length < 2) continue;
      file.versions.forEach((attachment, i) => map.set(attachment.id, i + 1));
    }
    return map;
  }, [files]);
  return (
    <AttachmentVersionsContext.Provider value={versions}>
      {children}
    </AttachmentVersionsContext.Provider>
  );
}

/** The 1-based version of an upload, when its file has more than one. */
export function useAttachmentVersions(): ReadonlyMap<string, number> | null {
  return use(AttachmentVersionsContext);
}
