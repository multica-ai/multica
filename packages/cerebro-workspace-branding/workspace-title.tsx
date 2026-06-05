"use client";

import { useEffect } from "react";
import { useCurrentWorkspace } from "@multica/core/paths";

export function CerebroWorkspaceTitle() {
  const workspace = useCurrentWorkspace();

  useEffect(() => {
    if (typeof document === "undefined" || !workspace?.name) return;
    const original = document.title;
    document.title = workspace.name;
    return () => {
      document.title = original;
    };
  }, [workspace?.name]);

  return null;
}
