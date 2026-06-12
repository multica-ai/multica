"use client";

import { useMemo } from "react";
import { useCurrentWorkspace } from "../paths";
import { deriveGitlabSettings, type GitlabDerivedSettings } from "./settings";

/**
 * Reads the GitLab feature flags off the current workspace's settings JSONB.
 * Components downstream should consult this hook rather than poking at
 * `workspace.settings` directly, so the per-flag fallback semantics
 * (see deriveGitlabSettings) stay consistent.
 */
export function useGitlabSettings(): GitlabDerivedSettings {
  const workspace = useCurrentWorkspace();
  return useMemo(() => deriveGitlabSettings(workspace), [workspace]);
}
