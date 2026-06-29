"use client";

import { useMemo } from "react";
import { useCurrentWorkspace } from "../paths";
import { deriveGitLabSettings, type GitLabSettings } from "./settings";

export function useGitLabSettings(): GitLabSettings {
  const workspace = useCurrentWorkspace();
  return useMemo(() => deriveGitLabSettings(workspace), [workspace]);
}
