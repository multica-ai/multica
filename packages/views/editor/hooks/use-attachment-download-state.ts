"use client";

import { useRef, useState } from "react";

export function useAttachmentDownloadState(
  onDownload: () => void | Promise<unknown>,
  attachmentKey = "",
) {
  const active = useRef(new Set<string>());
  const [pending, setPending] = useState<ReadonlySet<string>>(() => new Set());
  const handleDownload = () => {
    if (active.current.has(attachmentKey)) return;
    active.current.add(attachmentKey);
    const reset = () => {
      active.current.delete(attachmentKey);
      setPending((current) => {
        const next = new Set(current);
        next.delete(attachmentKey);
        return next;
      });
    };
    try {
      const result = onDownload();
      if (result && typeof result.then === "function") {
        setPending((current) => new Set(current).add(attachmentKey));
        void result.then(reset, reset);
      } else {
        active.current.delete(attachmentKey);
      }
    } catch (error) {
      reset();
      throw error;
    }
  };
  return { downloading: pending.has(attachmentKey), handleDownload };
}
