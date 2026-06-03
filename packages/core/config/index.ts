import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";

interface ConfigState {
  cdnDomain: string;
  allowSignup: boolean;
  googleClientId: string;
  daemonServerUrl: string;
  daemonAppUrl: string;
  // Self-host gate (#3433): when true, every "Create workspace" affordance
  // must be hidden. Defaults to false so unknown / older servers behave like
  // the managed-cloud case.
  workspaceCreationDisabled: boolean;
  /**
   * Generic-mode gate (NEXT_PUBLIC_GENERIC_MODE=true).
   *
   * When true the UI hides all dev/AI-specific surfaces so the tracker is
   * usable for any domain — not just software engineering.  Specifically:
   *   - Sidebar collapses to Tasks + Projects only (no Agents/Squads/
   *     Autopilots/Runtimes/Skills/Usage).
   *   - Settings page hides Repositories / GitHub / Labs tabs.
   *   - Default create-mode is "manual" (not "agent").
   *   - ExecutionLogSection / AgentLiveCard / PullRequestList are hidden in
   *     issue-detail.
   *   - Agents group is hidden in AssigneePicker.
   *   - "agents" scope tab is hidden in IssuesHeader.
   *   - Runtime step is skipped in onboarding.
   *   - BacklogAgentHintDialog is never shown.
   *
   * Defaults to false so self-hosted deployments that don't set the env var
   * keep full AI functionality.
   */
  genericMode: boolean;
  setCdnDomain: (domain: string) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    googleClientId?: string;
    workspaceCreationDisabled?: boolean;
  }) => void;
  setDaemonConfig: (config: {
    daemonServerUrl?: string;
    daemonAppUrl?: string;
  }) => void;
}

// Read NEXT_PUBLIC_GENERIC_MODE once at module load; no runtime setter needed
// because this is a build-time / deploy-time decision.
const _genericMode =
  typeof process !== "undefined" &&
  process.env.NEXT_PUBLIC_GENERIC_MODE === "true";

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  allowSignup: true,
  googleClientId: "",
  daemonServerUrl: "",
  daemonAppUrl: "",
  workspaceCreationDisabled: false,
  genericMode: _genericMode,
  setCdnDomain: (domain) => set({ cdnDomain: domain }),
  setAuthConfig: ({ allowSignup, googleClientId = "", workspaceCreationDisabled = false }) =>
    set({ allowSignup, googleClientId, workspaceCreationDisabled }),
  setDaemonConfig: ({ daemonServerUrl = "", daemonAppUrl = "" }) =>
    set({ daemonServerUrl, daemonAppUrl }),
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}
