"use client";

import { GitBranch } from "lucide-react";
import { SettingsPage, GitlabTab, type ExtraSettingsTab } from "@multica/views/settings";

// NEXT_PUBLIC_* envs are inlined into the client bundle at build time.
// Flipping this flag requires a rebuild + redeploy of the web app — unlike
// the server-side MULTICA_GITLAB_ENABLED, which can be toggled by a restart.
const gitlabEnabled = process.env.NEXT_PUBLIC_GITLAB_ENABLED === "true";

const extraWorkspaceTabs: ExtraSettingsTab[] = gitlabEnabled
  ? [
      {
        value: "gitlab",
        label: "GitLab",
        icon: GitBranch,
        content: <GitlabTab />,
      },
    ]
  : [];

export default function SettingsRoute() {
  return <SettingsPage extraWorkspaceTabs={extraWorkspaceTabs} />;
}
