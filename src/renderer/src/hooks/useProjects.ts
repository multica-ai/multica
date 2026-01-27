/**
 * Project management hook
 * Handles project CRUD operations and state
 */
import { useState, useCallback, useMemo } from 'react'
import type { MulticaProject, MulticaSession, ProjectWithSessions } from '../../../shared/types'
import { toast } from 'sonner'
import { getErrorMessage } from '../utils/error'

export interface ProjectsState {
  projects: MulticaProject[]
  sessionsByProject: Map<string, MulticaSession[]>
  projectsWithSessions: ProjectWithSessions[]
}

export interface ProjectsActions {
  loadProjectsAndSessions: () => Promise<void>
  createProject: (workingDirectory: string) => Promise<MulticaProject | null>
  toggleProjectExpanded: (projectId: string) => Promise<void>
  reorderProjects: (projectIds: string[]) => Promise<void>
  deleteProject: (projectId: string, onCurrentSessionAffected?: () => void) => Promise<void>
  setProjectsWithSessions: React.Dispatch<React.SetStateAction<ProjectWithSessions[]>>
}

export interface UseProjectsOptions {
  /** Callback when current session's project is deleted */
  currentSessionProjectId?: string | null
}

export function useProjects(options: UseProjectsOptions = {}): ProjectsState & ProjectsActions {
  const { currentSessionProjectId } = options

  const [projectsWithSessions, setProjectsWithSessions] = useState<ProjectWithSessions[]>([])

  // Derive projects and sessionsByProject from projectsWithSessions
  const projects = useMemo(() => projectsWithSessions.map((p) => p.project), [projectsWithSessions])

  const sessionsByProject = useMemo(() => {
    const map = new Map<string, MulticaSession[]>()
    for (const { project, sessions } of projectsWithSessions) {
      map.set(project.id, sessions)
    }
    return map
  }, [projectsWithSessions])

  const loadProjectsAndSessions = useCallback(async () => {
    try {
      const list = await window.electronAPI.listProjectsWithSessions()
      setProjectsWithSessions(list)
    } catch (err) {
      toast.error(`Failed to load projects: ${getErrorMessage(err)}`)
    }
  }, [])

  const createProject = useCallback(
    async (workingDirectory: string): Promise<MulticaProject | null> => {
      try {
        const project = await window.electronAPI.createProject(workingDirectory)
        await loadProjectsAndSessions()
        return project
      } catch (err) {
        toast.error(`Failed to create project: ${getErrorMessage(err)}`)
        return null
      }
    },
    [loadProjectsAndSessions]
  )

  const toggleProjectExpanded = useCallback(
    async (projectId: string) => {
      try {
        await window.electronAPI.toggleProjectExpanded(projectId)
        await loadProjectsAndSessions()
      } catch (err) {
        toast.error(`Failed to toggle project: ${getErrorMessage(err)}`)
      }
    },
    [loadProjectsAndSessions]
  )

  const reorderProjects = useCallback(
    async (projectIds: string[]) => {
      try {
        // Optimistic update: immediately reorder the local state
        setProjectsWithSessions((prev) => {
          const projectMap = new Map(prev.map((p) => [p.project.id, p]))
          return projectIds.map((id) => projectMap.get(id)!).filter(Boolean)
        })

        // Persist to backend
        await window.electronAPI.reorderProjects(projectIds)
      } catch (err) {
        toast.error(`Failed to reorder projects: ${getErrorMessage(err)}`)
        // Reload to restore correct order on error
        await loadProjectsAndSessions()
      }
    },
    [loadProjectsAndSessions]
  )

  const deleteProject = useCallback(
    async (projectId: string, onCurrentSessionAffected?: () => void) => {
      try {
        // Check if current session belongs to this project
        if (currentSessionProjectId === projectId && onCurrentSessionAffected) {
          onCurrentSessionAffected()
        }
        await window.electronAPI.deleteProject(projectId)
        await loadProjectsAndSessions()
      } catch (err) {
        toast.error(`Failed to delete project: ${getErrorMessage(err)}`)
      }
    },
    [currentSessionProjectId, loadProjectsAndSessions]
  )

  return {
    // State
    projects,
    sessionsByProject,
    projectsWithSessions,

    // Actions
    loadProjectsAndSessions,
    createProject,
    toggleProjectExpanded,
    reorderProjects,
    deleteProject,
    setProjectsWithSessions
  }
}
