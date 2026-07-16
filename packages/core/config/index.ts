import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";

// git-describe commit-distance suffix (-N-g<hash>) on a tag, e.g. v0.4.2-5-gabc1234.
const DESCRIBE_SUFFIX_RE = /-\d+-g[0-9a-f]{4,}$/;

/** officialBaseline returns the tag only when it is a trustworthy official
 *  release baseline: a clean vX.Y.Z-style tag carrying no commit-distance
 *  suffix (-N-g<hash>) or dirty marker. Dev builds, package-version
 *  fallbacks, hashes, and any non-v value map to "" so the UI shows
 *  "unavailable" instead of presenting a non-release as a baseline. Shared by
 *  the frontend- and backend-baseline setters so both components use one rule. */
export function officialBaseline(v?: string): string {
  const tag = (v ?? "").trim();
  if (!tag || tag === "dev") return "";
  if (!/^v\d/.test(tag)) return "";
  if (tag.includes("-dirty") || DESCRIBE_SUFFIX_RE.test(tag)) return "";
  return tag;
}

interface ConfigState {
  cdnDomain: string;
  // True when cdnDomain serves private content via time-bounded signed URLs
  // (CloudFront signing enabled server-side). Renderers must not treat a raw
  // storage URL on that domain as a loadable media source (MUL-3254).
  cdnSigned: boolean;
  allowSignup: boolean;
  googleClientId: string;
  daemonServerUrl: string;
  daemonAppUrl: string;
  // Self-host gate (#3433): when true, every "Create workspace" affordance
  // must be hidden. Defaults to false so unknown / older servers behave like
  // the managed-cloud case.
  workspaceCreationDisabled: boolean;
  featureFlags: Record<string, boolean>;
  // Official release baseline of the loaded frontend bundle (clean vX.Y.Z tag
  // compiled in at build time), or "" when the bundle was not stamped with a
  // trustworthy tag (dev build). Known synchronously at boot — no loading state.
  frontendBaseline: string;
  // Official release baseline of the running backend (from /api/config), or ""
  // when unavailable (dev/old server, fetch failure, or a non-baseline value).
  backendBaseline: string;
  // "loading" until the non-blocking /api/config request settles, so the UI
  // shows a loading state instead of prematurely marking the backend unavailable.
  backendBaselineStatus: "loading" | "settled";
  setCdnConfig: (config: { cdnDomain: string; cdnSigned?: boolean }) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    googleClientId?: string;
    workspaceCreationDisabled?: boolean;
  }) => void;
  setDaemonConfig: (config: {
    daemonServerUrl?: string;
    daemonAppUrl?: string;
  }) => void;
  setFeatureFlags: (flags?: Record<string, boolean>) => void;
  setFrontendBaseline: (baseline?: string) => void;
  setBackendBaseline: (baseline?: string) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  cdnSigned: false,
  allowSignup: true,
  googleClientId: "",
  daemonServerUrl: "",
  daemonAppUrl: "",
  workspaceCreationDisabled: false,
  featureFlags: {},
  frontendBaseline: "",
  backendBaseline: "",
  backendBaselineStatus: "loading",
  setCdnConfig: ({ cdnDomain, cdnSigned = false }) => set({ cdnDomain, cdnSigned }),
  setAuthConfig: ({ allowSignup, googleClientId = "", workspaceCreationDisabled = false }) =>
    set({ allowSignup, googleClientId, workspaceCreationDisabled }),
  setDaemonConfig: ({ daemonServerUrl = "", daemonAppUrl = "" }) =>
    set({ daemonServerUrl, daemonAppUrl }),
  setFeatureFlags: (flags = {}) => set({ featureFlags: { ...flags } }),
  setFrontendBaseline: (baseline) => set({ frontendBaseline: officialBaseline(baseline) }),
  setBackendBaseline: (baseline) =>
    set({ backendBaseline: officialBaseline(baseline), backendBaselineStatus: "settled" }),
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}

export function featureFlagEnabled(
  flags: Readonly<Record<string, boolean>> | undefined,
  key: string,
  defaultValue = false,
): boolean {
  return flags?.[key] ?? defaultValue;
}

export function useFeatureEnabled(key: string, defaultValue = false): boolean {
  return useConfigStore((state) =>
    featureFlagEnabled(state.featureFlags, key, defaultValue),
  );
}
