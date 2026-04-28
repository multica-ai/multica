import type { ModalsDict } from "./types";

export function createEnDict(): ModalsDict {
  return {
    common: {
      expand: "Expand",
      collapse: "Collapse",
      close: "Close",
      cancel: "Cancel",
      creating: "Creating...",
    },
    createIssue: {
      srTitle: "New Issue",
      breadcrumb: "New issue",
      titlePlaceholder: "Issue title",
      descriptionPlaceholder: "Add description...",
      moreOptions: "More options",
      parentChip: (identifier) => `Sub-issue of ${identifier}`,
      parentMenuItem: (identifier) => `Parent: ${identifier}`,
      setParent: "Set parent issue...",
      addSubIssue: "Add sub-issue...",
      removeParent: "Remove parent",
      removeParentAria: "Remove parent",
      childChip: (identifier) => `Sub-issue: ${identifier}`,
      removeChildAria: (identifier) => `Remove sub-issue ${identifier}`,
      parentPickerTitle: "Set parent issue",
      parentPickerDescription:
        "Search for an issue to set as the parent of the new issue",
      childPickerTitle: "Add sub-issue",
      childPickerDescription:
        "Search for an issue to add as a sub-issue of the new issue",
      submit: "Create Issue",
      submitting: "Creating...",
      successTitle: "Issue created",
      viewIssue: "View issue",
      failed: "Failed to create issue",
      failedSubIssuesAll: "Failed to link sub-issues",
      failedSubIssuesPartial: (failed, total) =>
        `Failed to link ${failed} of ${total} sub-issues`,
      failedUpdateStatus: "Failed to update status",
    },
    createProject: {
      srTitle: "New Project",
      breadcrumb: "New project",
      titlePlaceholder: "Project title",
      descriptionPlaceholder: "Add description...",
      chooseIcon: "Choose icon",
      leadFallback: "Lead",
      leadPlaceholder: "Assign lead...",
      noLead: "No lead",
      membersHeading: "Members",
      agentsHeading: "Agents",
      noResults: "No results",
      submit: "Create Project",
      submitting: "Creating...",
      success: "Project created",
      failed: "Failed to create project",
    },
    createWorkspace: {
      back: "Back",
      title: "Create a new workspace",
      description:
        "Workspaces are shared environments where teams can work on projects and issues.",
    },
    feedback: {
      title: "Feedback",
      description:
        "We'd love to hear what's working, what isn't, or what you'd like to see next.",
      placeholder:
        "Tell us about your experience, bugs you've found, or features you'd like to see…",
      waitForUploads: "Please wait for uploads to finish…",
      tooLong: "Message is too long",
      success: "Thanks for the feedback!",
      failedFallback: "Failed to send feedback",
      sending: "Sending…",
      submit: "Send feedback",
    },
    setParentIssue: {
      title: "Set parent issue",
      description: "Search for an issue to set as the parent of this issue",
      failed: "Failed to update issue",
      success: (identifier) => `Set ${identifier} as parent issue`,
    },
    addChildIssue: {
      title: "Add sub-issue",
      description: "Search for an issue to add as a sub-issue",
      failed: "Failed to add sub-issue",
      success: (identifier) => `Added ${identifier} as sub-issue`,
    },
    deleteIssueConfirm: {
      title: "Delete issue",
      description:
        "This will permanently delete this issue and all its comments. This action cannot be undone.",
      cancel: "Cancel",
      confirm: "Delete",
      deleting: "Deleting...",
      success: "Issue deleted",
      failed: "Failed to delete issue",
    },
    issuePicker: {
      placeholder: "Search issues...",
      searching: "Searching...",
      noResults: "No issues found.",
      typeToSearch: "Type to search issues",
    },
  };
}
