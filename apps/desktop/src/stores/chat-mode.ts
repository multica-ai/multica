import { create } from "zustand"

export type ChatMode = "select" | "local" | "remote"

interface ChatModeStore {
  mode: ChatMode
  setMode: (mode: ChatMode) => void
}

export const useChatModeStore = create<ChatModeStore>((set) => ({
  mode: "select",
  setMode: (mode) => set({ mode }),
}))
