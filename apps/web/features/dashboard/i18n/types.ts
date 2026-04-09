export type Locale = "en" | "zh";

export const locales: Locale[] = ["en", "zh"];

export const localeLabels: Record<Locale, string> = {
  en: "EN",
  zh: "中文",
};

export type DashboardDict = {
  sidebar: {
    inbox: string;
    myIssues: string;
    issues: string;
    projects: string;
    agents: string;
    runtimes: string;
    skills: string;
    settings: string;
    workspaces: string;
    createWorkspace: string;
    newIssue: string;
    logOut: string;
  };
  settings: {
    title: string;
    myAccount: string;
    profile: string;
    appearance: string;
    apiTokens: string;
    general: string;
    repositories: string;
    members: string;
  };
  appearance: {
    theme: string;
    light: string;
    dark: string;
    system: string;
    language: string;
  };
  account: {
    profile: string;
    displayName: string;
    email: string;
    updateProfile: string;
    updating: string;
    avatarUpdated: string;
    profileUpdated: string;
    failedUploadAvatar: string;
    failedUpdateProfile: string;
  };
  workspace: {
    workspaceName: string;
    description: string;
    aiContext: string;
    aiContextPlaceholder: string;
    save: string;
    saving: string;
    workspaceSaved: string;
    failedSave: string;
    dangerZone: string;
    leaveWorkspace: string;
    leaveDescription: string;
    leave: string;
    leaving: string;
    deleteWorkspace: string;
    deleteDescription: string;
    delete: string;
    deleting: string;
    confirm: string;
    cancel: string;
    failedLeave: string;
    failedDelete: string;
  };
  members: {
    title: string;
    inviteByEmail: string;
    emailPlaceholder: string;
    role: string;
    add: string;
    adding: string;
    removeMember: string;
    removeDescription: string;
    remove: string;
    removing: string;
    confirm: string;
    cancel: string;
    memberRemoved: string;
    failedRemove: string;
    failedInvite: string;
    roles: {
      owner: string;
      admin: string;
      member: string;
    };
    changeRole: string;
  };
  tokens: {
    title: string;
    description: string;
    tokenName: string;
    tokenNamePlaceholder: string;
    expiry: string;
    expiryOptions: {
      "7": string;
      "30": string;
      "90": string;
      "365": string;
      "-1": string;
    };
    create: string;
    creating: string;
    revoke: string;
    revoking: string;
    revokeConfirmTitle: string;
    revokeConfirmDescription: string;
    confirm: string;
    cancel: string;
    copy: string;
    copied: string;
    newTokenTitle: string;
    newTokenDescription: string;
    noTokens: string;
    created: string;
    lastUsed: string;
    neverUsed: string;
    failedLoad: string;
    failedCreate: string;
    failedRevoke: string;
  };
  repositories: {
    title: string;
    description: string;
    repoUrl: string;
    repoDescription: string;
    addRepo: string;
    save: string;
    saving: string;
    saved: string;
    failedSave: string;
    remove: string;
  };
  inbox: {
    title: string;
    empty: string;
    selectNotification: string;
    markRead: string;
    markAllRead: string;
    archive: string;
    archiveAll: string;
    archiveAllRead: string;
    archiveCompleted: string;
    types: {
      issue_assigned: string;
      unassigned: string;
      assignee_changed: string;
      status_changed: string;
      priority_changed: string;
      due_date_changed: string;
      new_comment: string;
      mentioned: string;
      review_requested: string;
      task_completed: string;
      task_failed: string;
      agent_blocked: string;
      agent_completed: string;
      reaction_added: string;
    };
  };
};
