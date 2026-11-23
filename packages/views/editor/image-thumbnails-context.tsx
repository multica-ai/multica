"use client";

import { createContext, use, type ReactNode } from "react";

const ImageThumbnailsContext = createContext(false);

/**
 * Inside an enabled provider, a read-only image whose attachment has a server
 * thumbnail shows that instead of the original. For surfaces that draw images
 * smaller than they are — the issue timeline's comment thumbnails — so the
 * page does not download and decode every original to paint them. The viewer,
 * download and copy keep the original.
 */
export function ImageThumbnailsProvider({
  enabled,
  children,
}: {
  enabled: boolean;
  children: ReactNode;
}) {
  return (
    <ImageThumbnailsContext.Provider value={enabled}>
      {children}
    </ImageThumbnailsContext.Provider>
  );
}

export function useImageThumbnails(): boolean {
  return use(ImageThumbnailsContext);
}
