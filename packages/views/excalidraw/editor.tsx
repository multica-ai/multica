"use client";

import { Excalidraw } from "@excalidraw/excalidraw";
import "@excalidraw/excalidraw/index.css";
import type { ExcalidrawProps } from "@excalidraw/excalidraw/types";
import { useTheme } from "@multica/ui/components/common/theme-provider";

// Adapted from the CAR-712 embedded editor. The host owns persistence and
// navigation; the editor stays inside its host's client-only lazy boundary.
export function ExcalidrawEditor(props: ExcalidrawProps) {
  const { resolvedTheme } = useTheme();
  return (
    <div data-slot="excalidraw-editor" className="h-full w-full">
      <Excalidraw theme={resolvedTheme === "dark" ? "dark" : "light"} {...props} />
    </div>
  );
}
