"use client";

import { useMemo } from "react";
import { useCurrentWorkspace } from "../paths";
import { deriveGitHubSettings, type GitHubSettings } from "./settings";

/**
 * Reads the GitHub feature flags off the current workspace's settings JSONB.
 * Components downstream should consult this hook rather than poking at
 * `workspace.settings` directly, so the per-flag fallback semantics
 * (see deriveGitHubSettings) stay consistent.
 */
export function useGitHubSettings(): GitHubSettings {
  const workspace = useCurrentWorkspace();
  return useMemo(() => deriveGitHubSettings(workspace), [workspace]);
}
