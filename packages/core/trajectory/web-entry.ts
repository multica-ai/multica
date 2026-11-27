import { createStore } from "zustand/vanilla";
export interface TrajectorySelection {
    kind: string;
    id: string;
    seq?: number;
    issueId?: string;
}
export interface TrajectoryNavigation {
    requested: boolean;
    selection?: TrajectorySelection | null;
    error?: string | null;
    select?: (selection: TrajectorySelection | null) => void;
    exit?: () => void;
    dismissError?: () => void;
}
export function createTrajectoryEntryStore(initial = false) {
    return createStore<{ active: boolean; setActive: (value: boolean) => void }>(set => ({ active: initial, setActive: active => set({ active }) }));
}
