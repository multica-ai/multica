"use client";

import * as React from "react";
import type { Editor } from "@tiptap/react";
import { authorCodeKey, authorCodePlugin } from "./author-code-plugin";

// useAuthorCodeStamper registers the author-code stamping plugin on a
// ContentEditor instance (FIR-2810). The plugin reads its config through a
// ref, so flipping the per-note toggle or a member-name change never needs a
// plugin re-registration (which would lose editor state).
export function useAuthorCodeStamper(
  editor: Editor | null,
  enabled: boolean,
  code: string,
): void {
  const config = React.useRef({ enabled, code });
  config.current = { enabled, code };

  React.useEffect(() => {
    if (!editor) return;
    editor.registerPlugin(authorCodePlugin(() => config.current));
    return () => {
      if (!editor.isDestroyed) {
        try {
          editor.unregisterPlugin(authorCodeKey);
        } catch {
          // editor already torn down — nothing to unregister.
        }
      }
    };
  }, [editor]);
}
