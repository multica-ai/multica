import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

interface IssueCollapseState {
  descriptionCollapsed?: boolean;
  subIssuesCollapsed?: boolean;
  collapsedTasks?: string[]; // 已折叠的 Agent Task ID 列表
  allTasksCollapsed?: boolean; // 新增：全局折叠标志
}

interface IssueDetailCollapseStore {
  // Key 为 issueId
  issueCollapseStates: Record<string, IssueCollapseState>;
  
  // 查询状态接口
  isDescriptionCollapsed: (issueId: string, defaultVal: boolean) => boolean;
  isSubIssuesCollapsed: (issueId: string, defaultVal: boolean) => boolean;
  isTaskCollapsed: (issueId: string, taskId: string, defaultVal: boolean) => boolean;
  
  // 更新状态接口
  setDescriptionCollapsed: (issueId: string, collapsed: boolean) => void;
  setSubIssuesCollapsed: (issueId: string, collapsed: boolean) => void;
  setTaskCollapsed: (issueId: string, taskId: string, collapsed: boolean) => void;
  
  // 全局一键控制
  toggleAllSections: (issueId: string, collapsed: boolean, taskIds?: string[]) => void;
}

export const useIssueDetailCollapseStore = create<IssueDetailCollapseStore>()(
  persist(
    (set, get) => ({
      issueCollapseStates: {},
      
      isDescriptionCollapsed: (issueId, defaultVal) => {
        const state = get().issueCollapseStates[issueId];
        return state?.descriptionCollapsed ?? defaultVal;
      },
      
      isSubIssuesCollapsed: (issueId, defaultVal) => {
        const state = get().issueCollapseStates[issueId];
        return state?.subIssuesCollapsed ?? defaultVal;
      },
      
      isTaskCollapsed: (issueId, taskId, defaultVal) => {
        const state = get().issueCollapseStates[issueId];
        if (state?.allTasksCollapsed === true) {
          return true;
        }
        if (state?.allTasksCollapsed === false) {
          return false;
        }
        return state?.collapsedTasks ? state.collapsedTasks.includes(taskId) : defaultVal;
      },
      
      setDescriptionCollapsed: (issueId, collapsed) =>
        set((s) => ({
          issueCollapseStates: {
            ...s.issueCollapseStates,
            [issueId]: { ...s.issueCollapseStates[issueId], descriptionCollapsed: collapsed }
          }
        })),
        
      setSubIssuesCollapsed: (issueId, collapsed) =>
        set((s) => ({
          issueCollapseStates: {
            ...s.issueCollapseStates,
            [issueId]: { ...s.issueCollapseStates[issueId], subIssuesCollapsed: collapsed }
          }
        })),
        
      setTaskCollapsed: (issueId, taskId, collapsed) =>
        set((s) => {
          const currentState = s.issueCollapseStates[issueId] ?? {};
          const currentCollapsedTasks = currentState.collapsedTasks ?? [];
          const nextCollapsedTasks = collapsed
            ? [...new Set([...currentCollapsedTasks, taskId])]
            : currentCollapsedTasks.filter((id) => id !== taskId);
            
          return {
            issueCollapseStates: {
              ...s.issueCollapseStates,
              [issueId]: { 
                ...currentState, 
                collapsedTasks: nextCollapsedTasks,
                allTasksCollapsed: undefined
              }
            }
          };
        }),
        
      toggleAllSections: (issueId, collapsed, taskIds = []) =>
        set((s) => ({
          issueCollapseStates: {
            ...s.issueCollapseStates,
            [issueId]: {
              descriptionCollapsed: collapsed,
              subIssuesCollapsed: collapsed,
              collapsedTasks: collapsed ? taskIds : [],
              allTasksCollapsed: collapsed
            }
          }
        }))
    }),
    {
      name: "multica_issue_detail_collapse",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    }
  )
);

registerForWorkspaceRehydration(() => useIssueDetailCollapseStore.persist.rehydrate());
