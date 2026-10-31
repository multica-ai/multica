"use client";

import { useQuery } from "@tanstack/react-query";
import { githubInstallationsOptions } from "../github/queries";
import { vcsConnectionsOptions } from "./queries";

/**
 * Whether the workspace has anywhere for a PR to come from: a GitHub App
 * installation or a Forgejo / Gitea / GitLab connection. `undefined` until
 * that is known; a failed lookup counts as none.
 */
export function useCodeHostConnected(wsId: string): boolean | undefined {
  const github = useQuery(githubInstallationsOptions(wsId));
  const vcs = useQuery(vcsConnectionsOptions(wsId));
  // The VCS response is not schema-parsed; read it defensively.
  const hasGitHub = (github.data?.installations?.length ?? 0) > 0;
  const hasVCS = (vcs.data?.connections?.length ?? 0) > 0;
  if (hasGitHub || hasVCS) return true;
  if (github.isPending || vcs.isPending) return undefined;
  return false;
}
