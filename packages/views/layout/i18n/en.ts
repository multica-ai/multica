import type { LayoutDict } from "./types";

export function createEnDict(): LayoutDict {
  return {
    nav: {
      inbox: "Inbox",
      chat: "Chat",
      myIssues: "My Issues",
      issues: "Issues",
      projects: "Projects",
      autopilots: "Autopilot",
      agents: "Agents",
      runtimes: "Runtimes",
      skills: "Skills",
      settings: "Settings",
    },
    groups: {
      pinned: "Pinned",
      workspace: "Workspace",
      configure: "Configure",
    },
    sidebar: {
      workspacesLabel: "Workspaces",
      createWorkspace: "Create workspace",
      pendingInvitations: "Pending invitations",
      workspaceFallback: "Workspace",
      join: "Join",
      decline: "Decline",
      logOut: "Log out",
      newIssue: "New Issue",
      unpin: "Unpin",
    },
    help: {
      triggerLabel: "Help",
      docs: "Docs",
      changeLog: "Change log",
      feedback: "Feedback",
    },
    loader: {
      loadingPrefix: "Loading ",
      loadingSuffix: "…",
      loadingWorkspace: "Loading workspace…",
    },
  };
}
