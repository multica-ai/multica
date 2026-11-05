import type {
  AgentHostBatchUserInfoInput,
  AgentHostBatchUserInfoResult,
  AgentHostListWorkspaceAgentProbesInput,
  AgentHostWorkspaceAgentProbesResult,
  PersistWriteResult,
  ReadWorkspaceAgentReadStateInput,
  ReadWorkspaceFileResult as AgentHostReadWorkspaceFileResult,
  WorkspaceAgentReadStateSnapshot,
  WriteWorkspaceAgentReadStateInput
} from "../shared/contracts/dto";
import type { WorkspaceUserProjectService } from "@tutti-os/workspace-user-project/contracts";

type AgentHostAsyncResult<T = any> = Promise<T>;
type AgentHostRecord = Record<string, unknown>;
type AgentHostUnsubscribe = () => void;

export type AgentHostClipboardApi = {
  writeImage?: (input: {
    data: string;
    mimeType: "image/png";
  }) => AgentHostAsyncResult<void>;
  writeText: (text: string) => AgentHostAsyncResult<void>;
};

export type AgentHostTerminalLoginHandle = {
  close: () => void;
  completion: Promise<"ready" | "timed_out" | "unavailable">;
};

export type AgentHostTerminalStartupAction = {
  type: "slash_command";
  commandName: string;
  readyText: string;
};

export type AgentHostTerminalLoginApi = {
  supportedStartupActionTypes?: readonly AgentHostTerminalStartupAction["type"][];
  run: (input: {
    agentTargetId: string;
    command: string;
    cwd?: string;
    startupAction?: AgentHostTerminalStartupAction;
  }) => AgentHostAsyncResult<AgentHostTerminalLoginHandle | void>;
};

export type AgentHostDebugApi = {
  logRuntimeDiagnostics: (
    payload: unknown
  ) => AgentHostAsyncResult<void> | void;
  logTerminalDiagnostics?: (
    payload: unknown
  ) => AgentHostAsyncResult<void> | void;
};

export type AgentHostFilesystemApi = AgentHostRecord & {
  readFileText: (payload: {
    path?: string;
    uri?: string;
  }) => AgentHostAsyncResult<{
    content: string;
    name?: string;
    path?: string;
  }>;
};

export type AgentHostMetaApi = AgentHostRecord & {
  appVersion?: string | null;
  isPackaged?: boolean;
  isTest?: boolean;
  mainPid?: number | null;
  platform?: string;
  workspaceId?: string;
};

export type AgentHostEnvironmentApi = AgentHostRecord & {
  getBaseUrl?: () => AgentHostAsyncResult<string>;
};

export interface AgentHostQuickPrompt {
  id: string;
  title: string;
  content: string;
  version: number;
  createdAtUnixMs: number;
  updatedAtUnixMs: number;
}

export interface AgentHostQuickPromptSnapshot {
  enabled: boolean;
  status: "idle" | "loading" | "ready" | "error";
  prompts: readonly AgentHostQuickPrompt[];
  error: string | null;
  revision: number;
  pendingMutationIds: readonly string[];
  orderMutationPending?: boolean;
}

export interface AgentHostQuickPromptsApi {
  ensureLoaded: (input?: { force?: boolean }) => AgentHostAsyncResult<void>;
  getSnapshot: () => AgentHostQuickPromptSnapshot;
  subscribe: (
    listener: (snapshot: AgentHostQuickPromptSnapshot) => void
  ) => AgentHostUnsubscribe;
  create: (input: {
    title: string;
    content: string;
  }) => AgentHostAsyncResult<AgentHostQuickPrompt>;
  update: (input: {
    id: string;
    title: string;
    content: string;
    expectedVersion: number;
  }) => AgentHostAsyncResult<AgentHostQuickPrompt>;
  remove: (input: {
    id: string;
    expectedVersion: number;
  }) => AgentHostAsyncResult<void>;
  move?: (input: {
    promptId: string;
    beforePromptId: string | null;
    expectedVersion: number;
  }) => AgentHostAsyncResult<readonly AgentHostQuickPrompt[]>;
}

export type AgentHostPersistenceApi = AgentHostRecord & {
  readWorkspaceAgentReadState: (
    input: ReadWorkspaceAgentReadStateInput
  ) => AgentHostAsyncResult<WorkspaceAgentReadStateSnapshot>;
  writeWorkspaceAgentReadState: (
    input: WriteWorkspaceAgentReadStateInput
  ) => AgentHostAsyncResult<PersistWriteResult>;
};

export type AgentHostToastHandle = {
  /** Settles the toast to a neutral, non-error, non-success tone. */
  info: (title: string, description?: string) => void;
  /** Settles the toast to the destructive tone. */
  reject: (title: string, description?: string) => void;
  /** Settles the toast to the success tone. */
  resolve: (title: string, description?: string) => void;
};

export type AgentHostToastApi = AgentHostRecord & {
  error: (title: string, description?: string) => void;
  info?: (title: string, description?: string) => void;
  /**
   * Opens one toast that stays mounted across async work: it shows busy
   * (spinner, no auto-dismiss) until the returned handle settles it in
   * place, at which point it starts auto-dismissing like any other toast.
   */
  loading?: (title: string) => AgentHostToastHandle;
  success?: (title: string, description?: string) => void;
};

export interface AgentHostSelectedFile {
  name?: string;
  path: string;
}

export interface AgentHostSelectFilesInput {
  allowDirectories?: boolean;
}

export interface AgentHostApplyWorkspaceGitPatchInput {
  allowBinary?: boolean;
  atomic?: boolean;
  cwd: string;
  diff: string;
  revert?: boolean;
  target?: "unstaged" | "staged" | "staged-and-unstaged";
}

export interface AgentHostApplyWorkspaceGitPatchResult {
  status: "success" | "partial-success" | "error";
  appliedPaths: string[];
  skippedPaths: string[];
  conflictedPaths: string[];
  errorCode?: "not-git-repo" | string;
  execOutput?: {
    command: string;
    stdout: string;
    stderr: string;
  };
}

export interface AgentHostResolveWorkspaceGitPatchSupportInput {
  cwd: string;
}

export interface AgentHostResolveWorkspaceGitPatchSupportResult {
  supported: boolean;
  root?: string;
  errorCode?: "not-git-repo" | string;
}

export interface AgentHostResolveSessionWorktreeSupportInput {
  agentTargetId: string;
  cwd: string;
}

export interface AgentHostResolveSessionWorktreeSupportResult {
  supported: boolean;
  root?: string;
  errorCode?:
    | "git-unavailable"
    | "not-git-repo"
    | "unsupported-repo-layout"
    | string;
}

export type AgentHostWorkspaceApi = AgentHostRecord & {
  applyGitPatch?: (
    input: AgentHostApplyWorkspaceGitPatchInput
  ) => AgentHostAsyncResult<AgentHostApplyWorkspaceGitPatchResult>;
  resolveGitPatchSupport?: (
    input: AgentHostResolveWorkspaceGitPatchSupportInput
  ) => AgentHostAsyncResult<AgentHostResolveWorkspaceGitPatchSupportResult>;
  resolveSessionWorktreeSupport?: (
    input: AgentHostResolveSessionWorktreeSupportInput
  ) => AgentHostAsyncResult<AgentHostResolveSessionWorktreeSupportResult>;
  copyPath?: (input: { path: string }) => AgentHostAsyncResult<void>;
  ensureDirectory: (input: { path: string }) => AgentHostAsyncResult<void>;
  readFile: (input: {
    path: string;
  }) => AgentHostAsyncResult<AgentHostReadWorkspaceFileResult>;
  selectContextEntries?: () => AgentHostAsyncResult<{ entries: unknown[] }>;
  selectDirectory: () => AgentHostAsyncResult<{ path: string } | null>;
  selectFiles: (
    input?: AgentHostSelectFilesInput
  ) => AgentHostAsyncResult<AgentHostSelectedFile[]>;
  writeFileText: (input: {
    content: string;
    path: string;
  }) => AgentHostAsyncResult<unknown>;
};

/** UI actions owned by the embedding host, independent of provider catalogs. */
export interface AgentHostComposerCommands {
  commands: readonly { name: string; label: string; description?: string }[];
  execute(input: { name: string; agentSessionId: string | null }): AgentHostAsyncResult<void>;
}

export interface AgentHostInputApi {
  composerCommands?: AgentHostComposerCommands;
  account?: AgentHostAccountApi;
  agentTargetSetup?: AgentHostAgentTargetSetupApi;
  clipboard: AgentHostClipboardApi;
  debug?: AgentHostDebugApi;
  filesystem: AgentHostFilesystemApi;
  meta?: AgentHostMetaApi;
  onHostEvent?: (listener: (event: any) => void) => AgentHostUnsubscribe;
  persistence?: AgentHostPersistenceApi;
  quickPrompts?: AgentHostQuickPromptsApi;
  runtime?: AgentHostEnvironmentApi;
  terminalLogin?: AgentHostTerminalLoginApi;
  toast?: AgentHostToastApi;
  userProjects?: AgentHostUserProjectsApi;
  workspace: AgentHostWorkspaceApi;
  workspaceAgentProbes?: AgentHostWorkspaceAgentProbesApi;
}

export type AgentHostApi = AgentHostInputApi;

export type AgentHostAccountApi = AgentHostRecord & {
  batchGetUserInfo: (
    input: AgentHostBatchUserInfoInput
  ) => AgentHostAsyncResult<AgentHostBatchUserInfoResult>;
  ensureProfiles?: (input: any) => AgentHostAsyncResult<any>;
};

export type AgentHostWorkspaceAgentProbesApi = AgentHostRecord & {
  list: (
    input: AgentHostListWorkspaceAgentProbesInput
  ) => AgentHostAsyncResult<AgentHostWorkspaceAgentProbesResult>;
};

export interface AgentHostAgentTargetInstallPlan {
  packageName: string;
  packageVersion: string;
  runner: "npm" | "pnpm" | "uv" | "binary";
  planDigest: string;
  installRoot: string;
}

export interface AgentHostAgentTargetSetupAction {
  actionId: string;
  clientActionId: string;
  kind: "install" | "authenticate";
  status: "queued" | "running" | "succeeded" | "failed" | "interrupted";
  phase:
    | "preparing"
    | "installing"
    | "verifying"
    | "probing"
    | "activating"
    | "authenticating"
    | "complete";
  errorCode: string | null;
  errorMessage: string | null;
}

export interface AgentHostAgentTargetSetupSnapshot {
  agentTargetId: string;
  status:
    | "ready"
    | "auth_required"
    | "not_installed"
    | "installing"
    | "authenticating"
    | "failed";
  runtimeSource: "local" | "managed" | null;
  runtimeVersion: string | null;
  reason: string | null;
  authMethods: AgentHostAgentTargetAuthMethod[];
  account: AgentHostAgentTargetAuthenticatedAccount | null;
  plan: AgentHostAgentTargetInstallPlan | null;
  action: AgentHostAgentTargetSetupAction | null;
}

export interface AgentHostAgentTargetAuthenticatedAccount {
  id: string;
  displayName: string;
  authMethodId: string;
  organization: string | null;
}

export interface AgentHostAgentTargetAuthMethod {
  id: string;
  name: string;
  description?: string | null;
  /** Provider-declared method kind (for example "terminal"). */
  type?: string | null;
  /** Ready-to-run interactive sign-in launch command for terminal-type methods. */
  terminalCommand?: string | null;
  /** Optional typed action submitted after the terminal runtime is ready. */
  terminalStartupAction?: AgentHostTerminalStartupAction | null;
}

export interface AgentHostAgentTargetSetupState {
  snapshot: AgentHostAgentTargetSetupSnapshot | null;
  loading: boolean;
  failed: boolean;
}

export interface AgentHostAgentTargetSetupWatch {
  getSnapshot: () => AgentHostAgentTargetSetupState;
  subscribe: (
    listener: (state: AgentHostAgentTargetSetupState) => void
  ) => AgentHostUnsubscribe;
  install: (input: {
    planDigest: string;
    clientActionId: string;
  }) => AgentHostAsyncResult<void>;
  authenticate: (input: {
    methodId: string;
    clientActionId: string;
  }) => AgentHostAsyncResult<void>;
  refresh: () => AgentHostAsyncResult<void>;
}

export type AgentHostAgentTargetSetupApi = AgentHostRecord & {
  watch: (input: { agentTargetId: string }) => AgentHostAgentTargetSetupWatch;
};

export type AgentProviderProbeListInput =
  AgentHostListWorkspaceAgentProbesInput;
export type AgentProviderProbeListResult = AgentHostWorkspaceAgentProbesResult;

export interface AgentHostUserProject {
  id: string;
  path: string;
  label: string;
  pinnedAtUnixMs: number;
  sectionKey?: string;
  createdAtUnixMs?: number;
  updatedAtUnixMs?: number;
  lastUsedAtUnixMs?: number;
}

export type AgentHostUserProjectsApi = AgentHostRecord & {
  service?: WorkspaceUserProjectService;
  checkPath?: (input: { path: string }) => AgentHostAsyncResult<{
    exists: boolean;
    isDirectory: boolean;
    path: string;
  }>;
  create?: (input: {
    name: string;
  }) => AgentHostAsyncResult<AgentHostUserProject>;
  getDefaultSelection?: () => AgentHostAsyncResult<{
    path: string | null;
  } | null>;
  list: () => AgentHostAsyncResult<{
    projects: AgentHostUserProject[];
  }>;
  move?: (input: {
    beforeProjectId: string | null;
    projectId: string;
  }) => AgentHostAsyncResult<void>;
  pin: (input: {
    pinned: boolean;
    projectId: string;
  }) => AgentHostAsyncResult<void>;
  subscribe?: (listener: () => void) => AgentHostUnsubscribe;
  prepareSelection?: (input: {
    projectLocked: boolean;
    selectedPath: string | null;
  }) => AgentHostAsyncResult<{
    isSelectedPathMissing: boolean;
    projects: AgentHostUserProject[];
    selection:
      | {
          kind: "clear";
          suppressedPath: string;
        }
      | {
          kind: "none";
        }
      | {
          kind: "select";
          path: string;
        };
  }>;
  remove?: (input: { path: string }) => AgentHostAsyncResult<void>;
  isNoProjectPath?: (input: { path: string }) => boolean;
  rememberDefaultSelection?: (input: {
    path: string | null;
  }) => AgentHostAsyncResult<void>;
  use: (input: { path: string }) => AgentHostAsyncResult<AgentHostUserProject>;
};

export interface AgentHostRuntimeApi {
  composerCommands?: AgentHostComposerCommands;
  account?: AgentHostAccountApi;
  agentTargetSetup?: AgentHostAgentTargetSetupApi;
  clipboard: AgentHostClipboardApi;
  debug?: AgentHostDebugApi;
  filesystem: AgentHostFilesystemApi;
  meta?: AgentHostMetaApi;
  onHostEvent?: (listener: (event: any) => void) => AgentHostUnsubscribe;
  persistence?: AgentHostPersistenceApi;
  quickPrompts?: AgentHostQuickPromptsApi;
  runtime?: AgentHostEnvironmentApi;
  terminalLogin?: AgentHostTerminalLoginApi;
  toast?: AgentHostToastApi;
  userProjects?: AgentHostUserProjectsApi;
  workspace: AgentHostWorkspaceApi;
  workspaceAgentProbes?: AgentHostWorkspaceAgentProbesApi;
}

export function toAgentHostRuntimeApi(
  hostApi: AgentHostInputApi | AgentHostRuntimeApi
): AgentHostRuntimeApi {
  return {
    composerCommands: hostApi.composerCommands,
    account: hostApi.account,
    agentTargetSetup: hostApi.agentTargetSetup,
    clipboard: hostApi.clipboard,
    debug: hostApi.debug,
    filesystem: hostApi.filesystem,
    meta: hostApi.meta,
    onHostEvent: hostApi.onHostEvent,
    persistence: hostApi.persistence,
    quickPrompts: hostApi.quickPrompts,
    runtime: hostApi.runtime,
    terminalLogin: hostApi.terminalLogin,
    toast: hostApi.toast,
    userProjects: hostApi.userProjects,
    workspace: hostApi.workspace,
    workspaceAgentProbes: hostApi.workspaceAgentProbes
  };
}
