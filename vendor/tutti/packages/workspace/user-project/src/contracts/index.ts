export interface WorkspaceUserProject {
  createdAtUnixMs?: number;
  id: string;
  label: string;
  lastUsedAtUnixMs?: number | null;
  path: string;
  pinnedAtUnixMs: number;
  sectionKey?: string;
  updatedAtUnixMs?: number;
}

export interface WorkspaceUserProjectPathCheck {
  exists: boolean;
  isDirectory: boolean;
  path: string;
}

export interface WorkspaceUserProjectDefaultSelection {
  path: string | null;
}

export interface WorkspaceUserProjectMoveInput {
  beforeProjectId: string | null;
  projectId: string;
}

export interface WorkspaceUserProjectPinInput {
  pinned: boolean;
  projectId: string;
}

export interface WorkspaceUserProjectSelectionPreparationInput {
  projectLocked: boolean;
  selectedPath: string | null;
}

export interface WorkspaceUserProjectSelectionPreparation {
  isSelectedPathMissing: boolean;
  projects: WorkspaceUserProject[];
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
}

export interface WorkspaceUserProjectApi {
  checkPath?(input: { path: string }): Promise<WorkspaceUserProjectPathCheck>;
  create?(input: { name: string }): Promise<WorkspaceUserProject>;
  getDefaultSelection?(): Promise<WorkspaceUserProjectDefaultSelection | null>;
  getSnapshot?(): Promise<WorkspaceUserProjectServiceSnapshot>;
  isNoProjectPath?(input: { path: string }): boolean;
  importDirectory?():
    | Promise<{ path: string } | null>
    | { path: string }
    | null;
  list(): Promise<{ projects: WorkspaceUserProject[] }>;
  move?(input: WorkspaceUserProjectMoveInput): Promise<void> | void;
  prepareSelection?(
    input: WorkspaceUserProjectSelectionPreparationInput
  ): Promise<WorkspaceUserProjectSelectionPreparation>;
  refresh?(): Promise<WorkspaceUserProjectServiceSnapshot>;
  remove?(input: { path: string }): Promise<void> | void;
  rememberDefaultSelection?(input: {
    path: string | null;
  }): Promise<void> | void;
  selectDirectory?():
    | Promise<{ path: string } | null>
    | { path: string }
    | null;
  subscribe?(
    listener: (snapshot?: WorkspaceUserProjectServiceSnapshot) => void
  ): () => void;
  use?(input: { path: string }): Promise<WorkspaceUserProject>;
}

export interface WorkspaceUserProjectServiceSnapshot {
  error: string | null;
  initialized: boolean;
  isLoading: boolean;
  isMutationPending?: boolean;
  projects: WorkspaceUserProject[];
  revision: number;
}

declare const workspaceUserProjectValtioStoreBrand: unique symbol;

export type WorkspaceUserProjectValtioStore =
  WorkspaceUserProjectServiceSnapshot & {
    readonly [workspaceUserProjectValtioStoreBrand]: true;
  };

export interface WorkspaceUserProjectService {
  checkProjectPath?(path: string): Promise<WorkspaceUserProjectPathCheck>;
  createProject?(name: string): Promise<WorkspaceUserProject>;
  ensureLoaded?(): Promise<void>;
  getDefaultSelection?(): Promise<WorkspaceUserProjectDefaultSelection | null>;
  getRevision?(): number;
  getSnapshot?(): WorkspaceUserProjectServiceSnapshot;
  isNoProjectPath?(path: string): boolean;
  pinProject?(input: WorkspaceUserProjectPinInput): Promise<void> | void;
  rememberNoProjectPath?(path: string | null | undefined): void;
  prepareSelection(
    input: WorkspaceUserProjectSelectionPreparationInput
  ): Promise<WorkspaceUserProjectSelectionPreparation>;
  refresh(): Promise<void>;
  registerProjectPath?(path: string): Promise<WorkspaceUserProject>;
  removeProjectPath?(path: string): Promise<void> | void;
  rememberDefaultSelection?(input: {
    path: string | null;
  }): Promise<void> | void;
  selectDirectory?():
    | Promise<{ path: string } | null>
    | { path: string }
    | null;
  store: WorkspaceUserProjectValtioStore;
  subscribe?(listener: () => void): () => void;
}

export type WorkspaceUserProjectCreationErrorCode =
  | "EACCES"
  | "EEXIST"
  | "ENOENT"
  | "EPERM"
  | "project_directory_already_exists"
  | "project_directory_permission_denied"
  | "project_documents_unavailable"
  | "project_name_invalid"
  | (string & {});
