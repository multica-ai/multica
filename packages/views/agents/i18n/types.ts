import type { AgentStatus } from "@multica/core/types";

export type AgentTaskStatusKey =
  | "queued"
  | "dispatched"
  | "running"
  | "completed"
  | "failed"
  | "cancelled";

export type AgentsDict = {
  page: {
    title: string;
    showActiveAgents: string;
    showArchivedAgents: string;
    emptyArchived: string;
    emptyActive: string;
    emptyAll: string;
    createAgent: string;
    selectToView: string;
  };
  list: {
    archived: string;
  };
  status: Record<AgentStatus, string>;
  detail: {
    archivedBanner: string;
    restore: string;
    archived: string;
    cloud: string;
    local: string;
    archiveAgent: string;
    archiveDialogTitle: string;
    archiveDialogDescription: (name: string) => string;
    archive: string;
    cancel: string;
  };
  tabs: {
    instructions: string;
    skills: string;
    tasks: string;
    env: string;
    customArgs: string;
    settings: string;
  };
  toasts: {
    agentUpdated: string;
    agentArchived: string;
    agentRestored: string;
    failedToUpdate: string;
    failedToArchive: string;
    failedToRestore: string;
    failedToCreate: string;
  };
  createDialog: {
    title: string;
    description: string;
    nameLabel: string;
    namePlaceholder: string;
    descriptionLabel: string;
    descriptionPlaceholder: string;
    visibilityLabel: string;
    workspaceTitle: string;
    workspaceHelp: string;
    privateTitle: string;
    privateHelp: string;
    runtimeLabel: string;
    runtimeFilterMine: string;
    runtimeFilterAll: string;
    cloudBadge: string;
    loadingRuntimes: string;
    noRuntimeAvailable: string;
    registerRuntimeFirst: string;
    cancel: string;
    creating: string;
    create: string;
  };
  instructions: {
    title: string;
    description: string;
    placeholder: string;
    characters: (n: number) => string;
    noInstructionsSet: string;
    save: string;
  };
  skills: {
    title: string;
    description: string;
    addSkill: string;
    localRuntimeNote: string;
    emptyTitle: string;
    emptyHelp: string;
    pickerTitle: string;
    pickerDescription: string;
    allAssigned: string;
    cancel: string;
    failedToAdd: string;
    failedToRemove: string;
  };
  tasks: {
    title: string;
    description: string;
    emptyTitle: string;
    emptyHelp: string;
    chatSession: string;
    autopilotRun: string;
    taskWithoutLinkedIssue: string;
    issueFallback: (id: string) => string;
    statusLabels: Record<AgentTaskStatusKey, string>;
    started: (when: string) => string;
    dispatched: (when: string) => string;
    completed: (when: string) => string;
    failed: (when: string) => string;
    queued: (when: string) => string;
  };
  settings: {
    avatarLabel: string;
    avatarUploadHint: string;
    nameLabel: string;
    nameRequired: string;
    descriptionLabel: string;
    descriptionPlaceholder: string;
    visibilityLabel: string;
    workspaceTitle: string;
    workspaceHelp: string;
    privateTitle: string;
    privateHelp: string;
    maxConcurrentTasksLabel: string;
    runtimeLabel: string;
    runtimeFilterMine: string;
    runtimeFilterAll: string;
    cloudBadge: string;
    noRuntimeAvailable: string;
    selectRuntime: string;
    saveChanges: string;
    avatarUpdated: string;
    avatarUploadFailed: string;
    settingsSaved: string;
    settingsSaveFailed: string;
  };
  env: {
    title: string;
    description: string;
    readOnlyDescription: string;
    add: string;
    keyPlaceholder: string;
    valuePlaceholder: string;
    noVariables: string;
    duplicateKeys: string;
    saved: string;
    saveFailed: string;
    save: string;
  };
  customArgs: {
    title: string;
    description: string;
    launchMode: string;
    yourArgs: string;
    add: string;
    placeholder: string;
    saved: string;
    saveFailed: string;
    save: string;
  };
  model: {
    label: string;
    discoveryFailed: string;
    selectRuntimeFirst: string;
    runtimeOfflineEnterManually: string;
    defaultProvider: string;
    defaultLabel: (label: string) => string;
    notSupportedTitle: string;
    notSupportedHelp: string;
    searchPlaceholder: string;
    discoveringModels: string;
    noModelsAvailable: string;
    use: (term: string) => string;
    clearSelection: string;
    custom: string;
    defaultBadge: string;
    modelFallback: string;
  };
  profileCard: {
    agentUnavailable: string;
    runtime: string;
    cloud: string;
    unknownRuntime: string;
    lastSeen: (when: string) => string;
    model: string;
    skills: string;
    owner: string;
    archived: string;
  };
};
