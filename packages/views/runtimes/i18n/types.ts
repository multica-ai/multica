export type RuntimesDict = {
  page: {
    title: string;
    selectToView: string;
    onlineCount: (online: number, total: number) => string;
  };
  list: {
    filterMine: string;
    filterAll: string;
    ownerLabel: string;
    allOwners: string;
    updateAvailable: string;
    bootstrapping: string;
    bootstrappingHint: string;
    emptyMine: string;
    emptyAllOwner: string;
    emptyAll: string;
    emptyHintBefore: string;
    emptyHintAfter: string;
  };
  status: {
    online: string;
    offline: string;
  };
  detail: {
    runtimeMode: string;
    provider: string;
    statusLabel: string;
    lastSeen: string;
    owner: string;
    device: string;
    daemonId: string;
    cliVersionHeading: string;
    tokenUsageHeading: string;
    metadataHeading: string;
    created: string;
    updated: string;
    deleteRuntime: string;
    deleteDialogDescription: (name: string) => string;
    cancel: string;
    deleting: string;
    delete: string;
    runtimeDeleted: string;
    runtimeDeleteFailed: string;
  };
  lastSeen: {
    never: string;
    justNow: string;
    minutesAgo: (n: number) => string;
    hoursAgo: (n: number) => string;
    daysAgo: (n: number) => string;
  };
  update: {
    cliVersionLabel: string;
    unknown: string;
    managedByDesktop: string;
    managedByDesktopTitle: string;
    latest: string;
    available: string;
    update: string;
    retry: string;
    failedToInitiate: string;
    unknownError: string;
    statuses: {
      pending: string;
      running: string;
      completed: string;
      failed: string;
      timeout: string;
    };
  };
  usage: {
    rangeLabels: {
      "7d": string;
      "30d": string;
      "90d": string;
    };
    inputLabel: string;
    outputLabel: string;
    cacheReadLabel: string;
    cacheWriteLabel: string;
    estimatedCost: (days: number) => string;
    noUsage: string;
    loading: string;
    tableDate: string;
    tableModel: string;
    tableInput: string;
    tableOutput: string;
    tableCacheR: string;
    tableCacheW: string;
  };
  charts: {
    activity: string;
    less: string;
    more: string;
    tokensSuffix: string;
    noActivity: string;
    hourlyDistribution: string;
    noTaskData: string;
    dailyTokenUsage: string;
    dailyEstimatedCost: string;
    tokenUsageByModel: string;
    tokensLabel: string;
    tokenInput: string;
    tokenOutput: string;
    tokenCacheRead: string;
    tokenCacheWrite: string;
    tokenTotal: string;
    costLabel: string;
    tasksLabel: string;
  };
};
