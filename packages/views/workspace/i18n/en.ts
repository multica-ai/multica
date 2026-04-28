import type { WorkspaceDict } from "./types";

export function createEnDict(): WorkspaceDict {
  return {
    createForm: {
      nameLabel: "Workspace Name",
      namePlaceholder: "My Workspace",
      urlLabel: "Workspace URL",
      slugPlaceholder: "my-workspace",
      submit: "Create workspace",
      submitting: "Creating...",
      slugFormatError: "Only lowercase letters, numbers, and hyphens",
      slugConflictError: "That workspace URL is already taken.",
      chooseDifferentSlug: "Choose a different workspace URL",
      createFailed: "Failed to create workspace",
    },
    noAccess: {
      title: "Workspace not available",
      description: "This workspace doesn't exist or you don't have access.",
      goToWorkspaces: "Go to my workspaces",
      signInDifferent: "Sign in as a different user",
    },
    newWorkspace: {
      back: "Back",
      logOut: "Log out",
      title: "Welcome to Multica",
      description:
        "One workspace where you and your AI teammates work side by side — taking issues, leaving comments, sharing the same context.",
      inviteHint: "You can invite teammates once your workspace is ready.",
    },
  };
}
