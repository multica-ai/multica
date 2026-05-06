import { describe, expect, it } from "vitest";

import {
  LOCAL_PRODUCT_CAPABILITIES,
  isLocalOnlyProduct,
  type ProductCapabilities,
} from "./local-product";

describe("local-only product capabilities", () => {
  it("disables remote and team-oriented cloud capabilities", () => {
    expect(LOCAL_PRODUCT_CAPABILITIES).toMatchObject({
      auth: {
        showLogin: false,
        showEmailLogin: false,
        showGoogleLogin: false,
        showApiTokens: false,
      },
      collaboration: {
        showMembers: false,
        showInvitations: false,
      },
      runtimes: {
        allowCloud: false,
        allowRemoteConnection: false,
      },
      feedback: {
        allowRemoteSubmission: false,
      },
    });
  });

  it("enables desktop-local capabilities", () => {
    expect(LOCAL_PRODUCT_CAPABILITIES).toMatchObject({
      runtimes: {
        allowLocal: true,
      },
      navigation: {
        showInbox: true,
      },
      settings: {
        showNotifications: true,
        showDaemon: true,
        showUpdates: true,
        showRepositories: true,
        showDiagnostics: true,
        showResetLocalData: true,
      },
      feedback: {
        showLocalDiagnostics: true,
      },
      automation: {
        allowAutopilots: true,
      },
    });
  });

  it("identifies local-only product mode", () => {
    expect(isLocalOnlyProduct(LOCAL_PRODUCT_CAPABILITIES)).toBe(true);
  });

  it("keeps the capability contract explicit", () => {
    const capabilities: ProductCapabilities = LOCAL_PRODUCT_CAPABILITIES;

    expect(capabilities).toEqual({
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
    });
  });
});
