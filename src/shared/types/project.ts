/**
 * Project types for Multica
 */

/**
 * MulticaProject - represents a project (working directory)
 * A project groups multiple sessions that work on the same codebase.
 */
export interface MulticaProject {
  // Identity
  id: string // UUID
  name: string // Display name (defaults to folder name)
  workingDirectory: string // Absolute path to project directory

  // Timestamps
  createdAt: string // ISO 8601
  updatedAt: string // Last activity time (updated when any session changes)

  // UI state (persisted)
  isExpanded: boolean // Sidebar expand/collapse state
  sortOrder: number // Manual sort order (lower = higher in list)

  // Runtime state (not persisted, populated on load)
  directoryExists?: boolean // true = exists, false = deleted/moved, undefined = not checked
  sessionCount?: number // Number of sessions (computed)
}

/**
 * Parameters for creating a new project
 */
export interface CreateProjectParams {
  workingDirectory: string
  name?: string // Optional, defaults to folder name
}

/**
 * Options for listing projects
 */
export interface ListProjectsOptions {
  limit?: number
  offset?: number
}

/**
 * Project with its sessions for sidebar display
 */
export interface ProjectWithSessions {
  project: MulticaProject
  sessions: import('./session').MulticaSession[]
}
