/**
 * Global modal state management using Zustand
 */
import { create } from 'zustand'
import type { MulticaSession, MulticaProject } from '../../../shared/types'

// Modal types
export type ModalType =
  | 'settings'
  | 'newSession'
  | 'archiveSession'
  | 'deleteSession'
  | 'deleteProject'
  | 'archivedSessions'

// Modal data types
export interface SettingsModalData {
  highlightAgent?: string // Agent ID to highlight (for missing dependency prompt)
  pendingFolder?: string // Folder path waiting to create session after agent install
}

export interface ArchivedSessionsModalData {
  projectId: string
  projectName: string
}

interface ModalDataMap {
  settings: SettingsModalData | undefined
  newSession: undefined
  archiveSession: MulticaSession
  deleteSession: MulticaSession
  deleteProject: MulticaProject
  archivedSessions: ArchivedSessionsModalData
}

interface ModalState<T extends ModalType> {
  isOpen: boolean
  data?: ModalDataMap[T]
}

interface ModalStore {
  modals: {
    [K in ModalType]: ModalState<K>
  }
  openModal: <T extends ModalType>(type: T, data?: ModalDataMap[T]) => void
  closeModal: (type: ModalType) => void
}

export const useModalStore = create<ModalStore>((set) => ({
  modals: {
    settings: { isOpen: false },
    newSession: { isOpen: false },
    archiveSession: { isOpen: false },
    deleteSession: { isOpen: false },
    deleteProject: { isOpen: false },
    archivedSessions: { isOpen: false }
  },
  openModal: (type, data) =>
    set((state) => ({
      modals: {
        ...state.modals,
        [type]: { isOpen: true, data }
      }
    })),
  closeModal: (type) =>
    set((state) => ({
      modals: {
        ...state.modals,
        [type]: { isOpen: false, data: undefined }
      }
    }))
}))

// Convenience selectors

export const useModal = <T extends ModalType>(type: T): ModalState<T> =>
  useModalStore((state) => state.modals[type] as ModalState<T>)

export const useOpenModal = (): ModalStore['openModal'] => useModalStore((state) => state.openModal)

export const useCloseModal = (): ModalStore['closeModal'] =>
  useModalStore((state) => state.closeModal)
