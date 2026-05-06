export type ProductMode = "local";

export type ProductCapabilities = {
  mode: ProductMode;
  auth: {
    showLogin: boolean;
    showEmailLogin: boolean;
    showGoogleLogin: boolean;
    showApiTokens: boolean;
  };
  collaboration: {
    showMembers: boolean;
    showInvitations: boolean;
    allowLeaveWorkspace: boolean;
  };
  runtimes: {
    allowLocal: boolean;
    allowCloud: boolean;
    allowRemoteConnection: boolean;
  };
  navigation: {
    showInbox: boolean;
  };
  settings: {
    showProfile: boolean;
    showAppearance: boolean;
    showNotifications: boolean;
    showDaemon: boolean;
    showUpdates: boolean;
    showSpaceGeneral: boolean;
    showRepositories: boolean;
    showLabs: boolean;
    showDiagnostics: boolean;
    showResetLocalData: boolean;
  };
  feedback: {
    allowRemoteSubmission: boolean;
    showLocalDiagnostics: boolean;
  };
  automation: {
    allowAutopilots: boolean;
    runWhileAppClosed: boolean;
    catchUpMissedRunsOnce: boolean;
  };
};

export const LOCAL_PRODUCT_CAPABILITIES: ProductCapabilities = {
  mode: "local",
  auth: {
    showLogin: false,
    showEmailLogin: false,
    showGoogleLogin: false,
    showApiTokens: false,
  },
  collaboration: {
    showMembers: false,
    showInvitations: false,
    allowLeaveWorkspace: false,
  },
  runtimes: {
    allowLocal: true,
    allowCloud: false,
    allowRemoteConnection: false,
  },
  navigation: {
    showInbox: true,
  },
  settings: {
    showProfile: true,
    showAppearance: true,
    showNotifications: true,
    showDaemon: true,
    showUpdates: true,
    showSpaceGeneral: true,
    showRepositories: true,
    showLabs: true,
    showDiagnostics: true,
    showResetLocalData: true,
  },
  feedback: {
    allowRemoteSubmission: false,
    showLocalDiagnostics: true,
  },
  automation: {
    allowAutopilots: true,
    runWhileAppClosed: false,
    catchUpMissedRunsOnce: true,
  },
};

export function isLocalOnlyProduct(capabilities: ProductCapabilities): boolean {
  return capabilities.mode === "local";
}
