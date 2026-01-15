/**
 * Global modal state management using Zustand
 */
import { create } from 'zustand'
import type { MulticaSession } from '../../../shared/types'

// Modal types
export type ModalType = 'settings' | 'newSession' | 'deleteSession'

// Modal data types
export interface SettingsModalData {
  highlightAgent?: string  // Agent ID to highlight (for missing dependency prompt)
  pendingFolder?: string   // Folder path waiting to create session after agent install
}

interface ModalDataMap {
  settings: SettingsModalData | undefined
  newSession: undefined
  deleteSession: MulticaSession
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
    deleteSession: { isOpen: false },
  },
  openModal: (type, data) =>
    set((state) => ({
      modals: {
        ...state.modals,
        [type]: { isOpen: true, data },
      },
    })),
  closeModal: (type) =>
    set((state) => ({
      modals: {
        ...state.modals,
        [type]: { isOpen: false, data: undefined },
      },
    })),
}))

// Convenience selectors
export const useModal = <T extends ModalType>(type: T) =>
  useModalStore((state) => state.modals[type] as ModalState<T>)

export const useOpenModal = () => useModalStore((state) => state.openModal)
export const useCloseModal = () => useModalStore((state) => state.closeModal)
